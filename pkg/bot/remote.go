package bot

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/2mf8/Go-Lagrange-Client/pkg/config"
	"github.com/2mf8/Go-Lagrange-Client/pkg/safe_ws"
	"github.com/2mf8/Go-Lagrange-Client/pkg/util"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/onebot"
	"github.com/fanliao/go-promise"
	"google.golang.org/protobuf/proto"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

//go:generate go run github.com/a8m/syncmap -o "gen_remote_map.go" -pkg bot -name RemoteMap "map[int64]map[string]*WsServer"
var (
	// RemoteServers key是botId，value是map（key是serverName，value是server）
	RemoteServers  RemoteMap
	wsprotocol     = 0
	forwardServers = make(map[string]*ForwardServer, 0)
	lock           = new(sync.RWMutex)
	SafeForwardMap = NewForwards()
)

type SafeForwards struct {
	Map map[string]*ForwardServer
	RW  *sync.RWMutex
}

type LifeTime struct {
	Time          int64  `json:"time,omitempty"`
	SelfId        int64  `json:"self_id,omitempty"`
	PostType      string `json:"post_type,omitempty"`
	MetaEventType string `json:"meta_event_type"`
	SubType       string `json:"sub_type"`
}

type actionResp struct {
	BotId   int64  `json:"bot_id,omitempty"`
	Ok      bool   `json:"ok,omitempty"`
	Status  any    `json:"status"`
	RetCode int32  `json:"retcode"`
	Data    any    `json:"data"`
	Echo    string `json:"echo"`
}

type WsServer struct {
	*safe_ws.SafeWebSocket        // 线程安全的ws
	*config.Plugin                // 服务器组配置
	wsUrl                  string // 随机抽中的url
	regexp                 *regexp.Regexp
}

type ForwardServer struct {
	Url           string
	Session       *safe_ws.ForwardSafeWebSocket
	Mu            sync.Mutex
	WaitingFrames map[string]*promise.Promise
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func NewForwards() *SafeForwards {
	return &SafeForwards{
		Map: forwardServers,
		RW:  lock,
	}
}

func (f *SafeForwards) SetForward(key string, value *ForwardServer) {
	f.RW.Lock()
	defer f.RW.Unlock()
	f.Map[key] = value
}

func (f *SafeForwards) GetForward(key string) *ForwardServer {
	f.RW.RLock()
	defer f.RW.RUnlock()
	return f.Map[key]
}

func (f *SafeForwards) DeleteForward(key string) {
	f.RW.Lock()
	defer f.RW.Unlock()
	delete(f.Map, key)
}

func UpgradeWebsocket(cli *client.QQClient, w http.ResponseWriter, r *http.Request) error {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	log.Infof("正向 WebSocket 已连接，远程地址为 %s", c.RemoteAddr().String())
	bs := fmt.Sprintf("%v_%s", cli.Uin, c.RemoteAddr().String())
	go ForwradConnect(cli, bs, c)
	return nil
}

func ForwradConnect(cli *client.QQClient, url string, conn *websocket.Conn) *ForwardServer {
	lt := &LifeTime{
		Time:          time.Now().Unix(),
		SelfId:        int64(cli.Uin),
		PostType:      "meta_event",
		MetaEventType: "lifecycle",
		SubType:       "connect",
	}
	messageHandler := func(messageType int, data []byte) {
		var frame = onebot.Frame{}
		var oframe = onebot.OFrame{}
		if messageType == websocket.TextMessage {
			err := json.Unmarshal(data, &frame)
			if err != nil {
				err := json.Unmarshal(data, &oframe)
				if err != nil {
					log.Errorf("failed to unmarshal websocket text message, err: %+v", err)
					return
				}
				frame.Action = oframe.Action
				frame.Echo = fmt.Sprintf("%v", oframe.Echo)
				frame.Params.Message = []*onebot.Message{
					{
						Type: "text",
						Data: map[string]string{
							"text": oframe.Params.Message,
						},
					},
				}
			}
			handleForwardOnebotApiFrame(cli, &frame, func(f onebot.Frame_FrameType) bool {
				return true
			}, &config.Plugin{
				Json:     true,
				Protocol: 1,
			}, SafeForwardMap.GetForward(url).Session)
		} else if messageType == websocket.BinaryMessage {
			err := json.Unmarshal(data, &frame)
			if err != nil {
				err := json.Unmarshal(data, &oframe)
				if err != nil {
					log.Errorf("failed to unmarshal websocket text message, err: %+v", err)
					return
				}
				frame.Action = oframe.Action
				frame.Echo = fmt.Sprintf("%v", oframe.Echo)
				frame.Params.Message = []*onebot.Message{
					{
						Type: "text",
						Data: map[string]string{
							"text": oframe.Params.Message,
						},
					},
				}
			}
			handleForwardOnebotApiFrame(cli, &frame, func(f onebot.Frame_FrameType) bool {
				return true
			}, &config.Plugin{
				Json:     true,
				Protocol: 1,
			}, SafeForwardMap.GetForward(url).Session)
		} else {
			log.Errorf("invalid websocket messageType: %+v", messageType)
			return
		}
	}
	closeHandler := func(code int, message string) {
		SafeForwardMap.GetForward(url).Session.Conn.Close()
		SafeForwardMap.DeleteForward(url)
		log.Infof("正向 WebSocket 已断连，断连 账号_地址 为 %s", url)
	}
	safews := safe_ws.NewForwardSafeWebSocket(conn, messageHandler, closeHandler)
	fs := &ForwardServer{
		Url:           url,
		Session:       safews,
		WaitingFrames: make(map[string]*promise.Promise),
	}
	SafeForwardMap.SetForward(url, fs)
	b, err := json.Marshal(lt)
	if err == nil {
		fs.Mu.Lock()
		defer fs.Mu.Unlock()
		fs.Session.ForwardSend(websocket.TextMessage, b)
	}
	return fs
}

func ForwrdSendMsg(cli *client.QQClient, f *onebot.Frame) {
	if gm, ok := f.PbData.(*onebot.Frame_GroupMessageEvent); ok {
		b, e := json.Marshal(gm.GroupMessageEvent)
		if e == nil {
			for i, _ := range SafeForwardMap.Map {
				if strings.HasPrefix(i, fmt.Sprintf("%v", cli.Uin)) {
					SafeForwardMap.GetForward(i).Mu.Lock()
					defer SafeForwardMap.GetForward(i).Mu.Unlock()
					SafeForwardMap.GetForward(i).Session.ForwardSend(websocket.TextMessage, b)
				}
			}
		}
	}
}

func ConnectUniversal(cli *client.QQClient) {
	botServers := map[string]*WsServer{}
	RemoteServers.Store(int64(cli.Uin), botServers)

	plugins := make([]*config.Plugin, 0)
	config.Plugins.Range(func(key string, value *config.Plugin) bool {
		plugins = append(plugins, value)
		return true
	})

	for _, group := range plugins {
		if group.Disabled || group.Urls == nil || len(group.Urls) < 1 {
			continue
		}
		serverGroup := *group
		util.SafeGo(func() {
			rand.Shuffle(len(serverGroup.Urls), func(i, j int) { serverGroup.Urls[i], serverGroup.Urls[j] = serverGroup.Urls[j], serverGroup.Urls[i] })
			urlIndex := 0 // 使用第几个url
			for IsClientExist(int64(cli.Uin)) {
				urlIndex = (urlIndex + 1) % len(serverGroup.Urls)
				serverUrl := serverGroup.Urls[urlIndex]
				log.Infof("开始连接Websocket服务器 [%s](%s)", serverGroup.Name, serverUrl)
				header := http.Header{}
				for k, v := range serverGroup.ExtraHeader {
					if v != nil {
						header[k] = v
					}
				}
				header["X-Self-ID"] = []string{strconv.FormatInt(int64(cli.Uin), 10)}
				header["X-Client-Role"] = []string{"Universal"}
				conn, _, err := websocket.DefaultDialer.Dial(serverUrl, header)
				if err != nil {
					log.Warnf("连接Websocket服务器 [%s](%s) 错误，5秒后重连: %v", serverGroup.Name, serverUrl, err)
					time.Sleep(5 * time.Second)
					continue
				}
				log.Infof("连接Websocket服务器成功 [%s](%s)", serverGroup.Name, serverUrl)
				closeChan := make(chan int, 1)
				safeWs := safe_ws.NewSafeWebSocket(conn, OnWsRecvMessage(cli, &serverGroup), func() {
					defer func() {
						_ = recover() // 可能多次触发
					}()
					closeChan <- 1
				})
				botServers[serverGroup.Name] = &WsServer{
					SafeWebSocket: safeWs,
					Plugin:        &serverGroup,
					wsUrl:         serverUrl,
					regexp:        nil,
				}
				if serverGroup.RegexFilter != "" {
					if regex, err := regexp.Compile(serverGroup.RegexFilter); err != nil {
						log.Errorf("failed to compile [%s], regex_filter: %s", serverGroup.Name, serverGroup.RegexFilter)
					} else {
						botServers[serverGroup.Name].regexp = regex
					}
				}
				util.SafeGo(func() {
					for IsClientExist(int64(cli.Uin)) {
						if err := safeWs.Send(websocket.PingMessage, []byte("ping")); err != nil {
							break
						}
						time.Sleep(5 * time.Second)
					}
				})
				<-closeChan
				close(closeChan)
				delete(botServers, serverGroup.Name)
				log.Warnf("Websocket 服务器 [%s](%s) 已断开，5秒后重连", serverGroup.Name, serverUrl)
				time.Sleep(5 * time.Second)
			}
			log.Errorf("client does not exist, close websocket, %+v", fmt.Sprintf("%v", cli.Uin))
		})
	}
}

func OnWsRecvMessage(cli *client.QQClient, plugin *config.Plugin) func(ws *safe_ws.SafeWebSocket, messageType int, data []byte) {
	wsprotocol = int(plugin.Protocol)
	apiFilter := map[onebot.Frame_FrameType]bool{}
	for _, apiType := range plugin.ApiFilter {
		apiFilter[onebot.Frame_FrameType(apiType)] = true
	}
	isApiAllow := func(frameType onebot.Frame_FrameType) bool {
		if len(apiFilter) == 0 {
			return true
		}
		return apiFilter[frameType]
	}

	return func(ws *safe_ws.SafeWebSocket, messageType int, data []byte) {
		if !IsClientExist(int64(cli.Uin)) {
			ws.Close()
			return
		}
		if messageType == websocket.PingMessage || messageType == websocket.PongMessage {
			return
		}
		if !cli.Online.Load() {
			log.Warnf("bot is not online, ignore API, %+v", fmt.Sprintf("%v", cli.Uin))
			return
		}
		var apiReq = onebot.Frame{}
		var oframe = onebot.OFrame{}
		switch messageType {
		case websocket.BinaryMessage:
			err := json.Unmarshal(data, &apiReq)
			if err != nil {
				log.Errorf("收到API text，解析错误 %v", err)
				err := json.Unmarshal(data, &oframe)
				if err != nil {
					log.Errorf("收到API text，解析错误 %v", err)
					return
				}
				apiReq.Action = oframe.Action
				apiReq.Echo = fmt.Sprintf("%v", oframe.Echo)
				apiReq.Params.Message = []*onebot.Message{
					{
						Type: "text",
						Data: map[string]string{
							"text": oframe.Params.Message,
						},
					},
				}
			}
		case websocket.TextMessage:
			err := json.Unmarshal(data, &apiReq)
			if err != nil {
				if err != nil {
					err := json.Unmarshal(data, &oframe)
					if err != nil {
						log.Errorf("收到API text，解析错误 %v", err)
						return
					}
					apiReq.Action = oframe.Action
					apiReq.Echo = fmt.Sprintf("%v", oframe.Echo)
					apiReq.Params.Message = []*onebot.Message{
						{
							Type: "text",
							Data: map[string]string{
								"text": oframe.Params.Message,
							},
						},
					}
				}
			}
		}

		log.Debugf("收到 apiReq 信息, %+v", util.MustMarshal(apiReq))
		handleOnebotApiFrame(cli, &apiReq, isApiAllow, plugin, ws)
	}
}

func handleOnebotApiFrame(cli *client.QQClient, req *onebot.Frame, isApiAllow func(onebot.Frame_FrameType) bool, plugin *config.Plugin, ws *safe_ws.SafeWebSocket) {
	resp := &onebot.Frame{
		Echo: req.Echo,
	}
	if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_group_msg)] {
		reqData := &onebot.Frame_SendGroupMsgReq{
			SendGroupMsgReq: &onebot.SendGroupMsgReq{
				GroupId:    req.Params.GroupId,
				Message:    req.Params.Message,
				AutoEscape: req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendGroupMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendGroupMsgReq); !resp.Ok {
			return
		}
		r := HandleSendGroupMsg(cli, reqData.SendGroupMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_private_msg)] {
		reqData := &onebot.Frame_SendPrivateMsgReq{
			SendPrivateMsgReq: &onebot.SendPrivateMsgReq{
				UserId:     req.Params.UserId,
				Message:    req.Params.Message,
				AutoEscape: req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendPrivateMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendPrivateMsgReq); !resp.Ok {
			return
		}
		ra := &onebot.Frame_SendPrivateMsgResp{
			SendPrivateMsgResp: HandleSendPrivateMsg(cli, reqData.SendPrivateMsgReq),
		}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    ra.SendPrivateMsgResp,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_msg)] {
		reqData := &onebot.Frame_SendMsgReq{
			SendMsgReq: &onebot.SendMsgReq{
				MessageType: req.Params.MessageType,
				GroupId:     req.Params.GroupId,
				UserId:      req.Params.UserId,
				Message:     req.Params.Message,
				AutoEscape:  req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendPrivateMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendPrivateMsgReq); !resp.Ok {
			return
		}
		ra := &onebot.Frame_SendMsgResp{
			SendMsgResp: HandleSendMsg(cli, reqData.SendMsgReq),
		}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    ra.SendMsgResp,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_msg)] {
		reqData := &onebot.Frame_GetMsgReq{
			GetMsgReq: &onebot.GetMsgReq{
				MessageId: int32(req.Params.MessageId),
			},
		}
		resp.FrameType = onebot.Frame_TGetMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetMsgReq); !resp.Ok {
			return
		}
		r := HandleGetMsg(cli, reqData.GetMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_delete_msg)] {
		reqData := &onebot.Frame_DeleteMsgReq{
			DeleteMsgReq: &onebot.DeleteMsgReq{
				MessageId: int32(req.Params.MessageId),
			},
		}
		resp.FrameType = onebot.Frame_TDeleteMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TDeleteMsgReq); !resp.Ok {
			return
		}
		r := HandleDeletMsg(cli, reqData.DeleteMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_kick)] {
		reqData := &onebot.Frame_SetGroupKickReq{
			SetGroupKickReq: &onebot.SetGroupKickReq{
				GroupId:          req.Params.GroupId,
				UserId:           req.Params.UserId,
				RejectAddRequest: req.Params.RejectAddRequest,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupKickResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupKickReq); !resp.Ok {
			return
		}
		r := HandleSetGroupKick(cli, reqData.SetGroupKickReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_ban)] {
		reqData := &onebot.Frame_SetGroupBanReq{
			SetGroupBanReq: &onebot.SetGroupBanReq{
				GroupId:  req.Params.GroupId,
				UserId:   req.Params.UserId,
				Duration: int32(req.Params.Duration),
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupBanResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupBanReq); !resp.Ok {
			return
		}
		r := HandleSetGroupBan(cli, reqData.SetGroupBanReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_whole_ban)] {
		reqData := &onebot.Frame_SetGroupWholeBanReq{
			SetGroupWholeBanReq: &onebot.SetGroupWholeBanReq{
				GroupId: req.Params.GroupId,
				Enable:  req.Params.Enable,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupWholeBanResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupWholeBanReq); !resp.Ok {
			return
		}
		r := HandleSetGroupWholeBan(cli, reqData.SetGroupWholeBanReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_card)] {
		reqData := &onebot.Frame_SetGroupCardReq{
			SetGroupCardReq: &onebot.SetGroupCardReq{
				GroupId: req.Params.GroupId,
				UserId:  req.Params.UserId,
				Card:    req.Params.Card,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupCardResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupCardReq); !resp.Ok {
			return
		}
		r := HandleSetGroupCard(cli, reqData.SetGroupCardReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_name)] {
		reqData := &onebot.Frame_SetGroupNameReq{
			SetGroupNameReq: &onebot.SetGroupNameReq{
				GroupId:   req.Params.GroupId,
				GroupName: req.Params.GroupName,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupNameResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupNameReq); !resp.Ok {
			return
		}
		r := HandleSetGroupName(cli, reqData.SetGroupNameReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_leave)] {
		reqData := &onebot.Frame_SetGroupLeaveReq{
			SetGroupLeaveReq: &onebot.SetGroupLeaveReq{
				GroupId:   req.Params.GroupId,
				IsDismiss: req.Params.IsDismiss,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupLeaveResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupLeaveReq); !resp.Ok {
			return
		}
		r := HandleSetGroupLeave(cli, reqData.SetGroupLeaveReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_info)] {
		reqData := &onebot.Frame_GetGroupInfoReq{
			GetGroupInfoReq: &onebot.GetGroupInfoReq{
				GroupId: req.Params.GroupId,
				NoCache: req.Params.NoCache,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupInfoReq); !resp.Ok {
			return
		}
		r := HandleGetGroupInfo(cli, reqData.GetGroupInfoReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_member_info)] {
		reqData := &onebot.Frame_GetGroupMemberInfoReq{
			GetGroupMemberInfoReq: &onebot.GetGroupMemberInfoReq{
				GroupId: req.Params.GroupId,
				UserId:  req.Params.UserId,
				NoCache: req.Params.NoCache,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupMemberInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberInfoReq); !resp.Ok {
			return
		}
		r := HandleGetGroupMemberInfo(cli, reqData.GetGroupMemberInfoReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_group_poke)] {
		reqData := &onebot.Frame_SendGroupPokeReq{
			SendGroupPokeReq: &onebot.SendGroupPokeReq{
				GroupId: req.Params.GroupId,
				ToUin:   req.Params.ToUin,
			},
		}
		resp.FrameType = onebot.Frame_TSendGroupPokeResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendGroupPokeReq); !resp.Ok {
			return
		}
		r := HandleSendGroupPoke(cli, reqData.SendGroupPokeReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_friend_poke)] {
		reqData := &onebot.Frame_SendFriendPokeReq{
			SendFriendPokeReq: &onebot.SendFriendPokeReq{
				ToUin:  req.Params.ToUin,
				AioUin: req.Params.AioUin,
			},
		}
		resp.FrameType = onebot.Frame_TSendFriendPokeResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendFriendPokeReq); !resp.Ok {
			return
		}
		r := HandleSendFriendPoke(cli, reqData.SendFriendPokeReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_version_info)] {
		resp.FrameType = onebot.Frame_TGetVersionInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetVersionInfoReq); !resp.Ok {
			return
		}
		r := &onebot.GetVersionInfoResp{}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_friend_add_request)] {
		reqData := &onebot.Frame_SetFriendAddRequestReq{
			SetFriendAddRequestReq: &onebot.SetFriendAddRequestReq{
				Flag:    req.Params.Flag,
				Approve: req.Params.Approve,
				Remark:  req.Params.Remark,
			},
		}
		resp.FrameType = onebot.Frame_TSetFriendAddRequestResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetFriendAddRequestReq); !resp.Ok {
			return
		}
		r := HandleSetFriendAddRequest(cli, reqData.SetFriendAddRequestReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_list)] {
		reqData := &onebot.Frame_GetGroupListReq{
			GetGroupListReq: &onebot.GetGroupListReq{},
		}
		resp.FrameType = onebot.Frame_TGetGroupListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupListReq); !resp.Ok {
			return
		}
		r := HandleGetGroupList(cli, reqData.GetGroupListReq)
		if r == nil {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    nil,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		} else {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    &r.Group,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		}
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_member_list)] {
		reqData := &onebot.Frame_GetGroupMemberListReq{
			GetGroupMemberListReq: &onebot.GetGroupMemberListReq{
				GroupId: req.Params.GroupId,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupMemberListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberListReq); !resp.Ok {
			return
		}
		r := HandleGetGroupMemberList(cli, reqData.GetGroupMemberListReq)
		if r == nil {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    nil,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		} else {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    &r.GroupMember,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		}
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_add_request)] {
		reqData := &onebot.Frame_SetGroupAddRequestReq{
			SetGroupAddRequestReq: &onebot.SetGroupAddRequestReq{
				Flag:    req.Params.Flag,
				SubType: req.Params.SubType,
				Type:    req.Params.Type,
				Approve: req.Params.Approve,
				Reason:  req.Params.Reason,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupAddRequestResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupAddRequestReq); !resp.Ok {
			return
		}
		r := HandleSetGroupAddRequest(cli, reqData.SetGroupAddRequestReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_forward_msg)] {
		reqData := &onebot.Frame_SendForwardMsgReq{
			SendForwardMsgReq: &onebot.SendForwardMsgReq{
				GroupId:  req.Params.GroupId,
				UserId:   req.Params.UserId,
				Messages: req.Params.Messages,
			},
		}
		resp.FrameType = onebot.Frame_TSendForwardMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendForwardMsgReq); !resp.Ok {
			return
		}
		r := HandleSendForwardMsg(cli, reqData.SendForwardMsgReq)
		if r == nil {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    nil,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		} else {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    &r.ResId,
				Echo:    req.Echo,
			}
			sendActionRespData(data, plugin, ws)
		}
	} else {
		data := &actionResp{
			Status:  "failure",
			RetCode: -1,
			Data:    fmt.Sprintf("请求 %s 失败，%s 不存在或未实现", req.Action, req.Action),
			Echo:    req.Echo,
		}
		sendActionRespData(data, plugin, ws)
	}
}

func handleForwardOnebotApiFrame(cli *client.QQClient, req *onebot.Frame, isApiAllow func(onebot.Frame_FrameType) bool, plugin *config.Plugin, ws *safe_ws.ForwardSafeWebSocket) {
	resp := &onebot.Frame{
		Echo: req.Echo,
	}
	if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_group_msg)] {
		reqData := &onebot.Frame_SendGroupMsgReq{
			SendGroupMsgReq: &onebot.SendGroupMsgReq{
				GroupId:    req.Params.GroupId,
				Message:    req.Params.Message,
				AutoEscape: req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendGroupMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendGroupMsgReq); !resp.Ok {
			return
		}
		r := HandleSendGroupMsg(cli, reqData.SendGroupMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_private_msg)] {
		reqData := &onebot.Frame_SendPrivateMsgReq{
			SendPrivateMsgReq: &onebot.SendPrivateMsgReq{
				UserId:     req.Params.UserId,
				Message:    req.Params.Message,
				AutoEscape: req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendPrivateMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendPrivateMsgReq); !resp.Ok {
			return
		}
		ra := &onebot.Frame_SendPrivateMsgResp{
			SendPrivateMsgResp: HandleSendPrivateMsg(cli, reqData.SendPrivateMsgReq),
		}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    ra.SendPrivateMsgResp,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_send_msg)] {
		reqData := &onebot.Frame_SendMsgReq{
			SendMsgReq: &onebot.SendMsgReq{
				MessageType: req.Params.MessageType,
				GroupId:     req.Params.GroupId,
				UserId:      req.Params.UserId,
				Message:     req.Params.Message,
				AutoEscape:  req.Params.AutoEscape,
			},
		}
		resp.FrameType = onebot.Frame_TSendPrivateMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendPrivateMsgReq); !resp.Ok {
			return
		}
		ra := &onebot.Frame_SendMsgResp{
			SendMsgResp: HandleSendMsg(cli, reqData.SendMsgReq),
		}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    ra.SendMsgResp,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_msg)] {
		reqData := &onebot.Frame_GetMsgReq{
			GetMsgReq: &onebot.GetMsgReq{
				MessageId: int32(req.Params.MessageId),
			},
		}
		resp.FrameType = onebot.Frame_TGetMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetMsgReq); !resp.Ok {
			return
		}
		r := HandleGetMsg(cli, reqData.GetMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_delete_msg)] {
		reqData := &onebot.Frame_DeleteMsgReq{
			DeleteMsgReq: &onebot.DeleteMsgReq{
				MessageId: int32(req.Params.MessageId),
			},
		}
		resp.FrameType = onebot.Frame_TDeleteMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TDeleteMsgReq); !resp.Ok {
			return
		}
		r := HandleDeletMsg(cli, reqData.DeleteMsgReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_kick)] {
		reqData := &onebot.Frame_SetGroupKickReq{
			SetGroupKickReq: &onebot.SetGroupKickReq{
				GroupId:          req.Params.GroupId,
				UserId:           req.Params.UserId,
				RejectAddRequest: req.Params.RejectAddRequest,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupKickResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupKickReq); !resp.Ok {
			return
		}
		r := HandleSetGroupKick(cli, reqData.SetGroupKickReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_ban)] {
		reqData := &onebot.Frame_SetGroupBanReq{
			SetGroupBanReq: &onebot.SetGroupBanReq{
				GroupId:  req.Params.GroupId,
				UserId:   req.Params.UserId,
				Duration: int32(req.Params.Duration),
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupBanResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupBanReq); !resp.Ok {
			return
		}
		r := HandleSetGroupBan(cli, reqData.SetGroupBanReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_whole_ban)] {
		reqData := &onebot.Frame_SetGroupWholeBanReq{
			SetGroupWholeBanReq: &onebot.SetGroupWholeBanReq{
				GroupId: req.Params.GroupId,
				Enable:  req.Params.Enable,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupWholeBanResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupWholeBanReq); !resp.Ok {
			return
		}
		r := HandleSetGroupWholeBan(cli, reqData.SetGroupWholeBanReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_card)] {
		reqData := &onebot.Frame_SetGroupCardReq{
			SetGroupCardReq: &onebot.SetGroupCardReq{
				GroupId: req.Params.GroupId,
				UserId:  req.Params.UserId,
				Card:    req.Params.Card,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupCardResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupCardReq); !resp.Ok {
			return
		}
		r := HandleSetGroupCard(cli, reqData.SetGroupCardReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_name)] {
		reqData := &onebot.Frame_SetGroupNameReq{
			SetGroupNameReq: &onebot.SetGroupNameReq{
				GroupId:   req.Params.GroupId,
				GroupName: req.Params.GroupName,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupNameResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupNameReq); !resp.Ok {
			return
		}
		r := HandleSetGroupName(cli, reqData.SetGroupNameReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_leave)] {
		reqData := &onebot.Frame_SetGroupLeaveReq{
			SetGroupLeaveReq: &onebot.SetGroupLeaveReq{
				GroupId:   req.Params.GroupId,
				IsDismiss: req.Params.IsDismiss,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupLeaveResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupLeaveReq); !resp.Ok {
			return
		}
		r := HandleSetGroupLeave(cli, reqData.SetGroupLeaveReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_info)] {
		reqData := &onebot.Frame_GetGroupInfoReq{
			GetGroupInfoReq: &onebot.GetGroupInfoReq{
				GroupId: req.Params.GroupId,
				NoCache: req.Params.NoCache,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupInfoReq); !resp.Ok {
			return
		}
		r := HandleGetGroupInfo(cli, reqData.GetGroupInfoReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_member_info)] {
		reqData := &onebot.Frame_GetGroupMemberInfoReq{
			GetGroupMemberInfoReq: &onebot.GetGroupMemberInfoReq{
				GroupId: req.Params.GroupId,
				UserId:  req.Params.UserId,
				NoCache: req.Params.NoCache,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupMemberInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberInfoReq); !resp.Ok {
			return
		}
		r := HandleGetGroupMemberInfo(cli, reqData.GetGroupMemberInfoReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_group_poke)] {
		reqData := &onebot.Frame_SendGroupPokeReq{
			SendGroupPokeReq: &onebot.SendGroupPokeReq{
				GroupId: req.Params.GroupId,
				ToUin:   req.Params.ToUin,
			},
		}
		resp.FrameType = onebot.Frame_TSendGroupPokeResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendGroupPokeReq); !resp.Ok {
			return
		}
		r := HandleSendGroupPoke(cli, reqData.SendGroupPokeReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_friend_poke)] {
		reqData := &onebot.Frame_SendFriendPokeReq{
			SendFriendPokeReq: &onebot.SendFriendPokeReq{
				ToUin:  req.Params.ToUin,
				AioUin: req.Params.AioUin,
			},
		}
		resp.FrameType = onebot.Frame_TSendFriendPokeResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendFriendPokeReq); !resp.Ok {
			return
		}
		r := HandleSendFriendPoke(cli, reqData.SendFriendPokeReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_version_info)] {
		resp.FrameType = onebot.Frame_TGetVersionInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetVersionInfoReq); !resp.Ok {
			return
		}
		r := &onebot.GetVersionInfoResp{}
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_friend_add_request)] {
		reqData := &onebot.Frame_SetFriendAddRequestReq{
			SetFriendAddRequestReq: &onebot.SetFriendAddRequestReq{
				Flag:    req.Params.Flag,
				Approve: req.Params.Approve,
				Remark:  req.Params.Remark,
			},
		}
		resp.FrameType = onebot.Frame_TSetFriendAddRequestResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetFriendAddRequestReq); !resp.Ok {
			return
		}
		r := HandleSetFriendAddRequest(cli, reqData.SetFriendAddRequestReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_list)] {
		reqData := &onebot.Frame_GetGroupListReq{
			GetGroupListReq: &onebot.GetGroupListReq{},
		}
		resp.FrameType = onebot.Frame_TGetGroupListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupListReq); !resp.Ok {
			return
		}
		r := HandleGetGroupList(cli, reqData.GetGroupListReq)
		if r == nil {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    nil,
				Echo:    req.Echo,
			}
			sendForwardActionRespData(data, plugin, ws)
		} else {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    &r.Group,
				Echo:    req.Echo,
			}
			sendForwardActionRespData(data, plugin, ws)
		}
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_get_group_member_list)] {
		reqData := &onebot.Frame_GetGroupMemberListReq{
			GetGroupMemberListReq: &onebot.GetGroupMemberListReq{
				GroupId: req.Params.GroupId,
			},
		}
		resp.FrameType = onebot.Frame_TGetGroupMemberListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberListReq); !resp.Ok {
			return
		}
		r := HandleGetGroupMemberList(cli, reqData.GetGroupMemberListReq)
		if r == nil {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    nil,
				Echo:    req.Echo,
			}
			sendForwardActionRespData(data, plugin, ws)
		} else {
			data := &actionResp{
				Status:  "ok",
				RetCode: 0,
				Data:    &r.GroupMember,
				Echo:    req.Echo,
			}
			sendForwardActionRespData(data, plugin, ws)
		}
	} else if req.Action == onebot.ActionType_name[int32(onebot.ActionType_set_group_add_request)] {
		reqData := &onebot.Frame_SetGroupAddRequestReq{
			SetGroupAddRequestReq: &onebot.SetGroupAddRequestReq{
				Flag:    req.Params.Flag,
				SubType: req.Params.SubType,
				Type:    req.Params.Type,
				Approve: req.Params.Approve,
				Reason:  req.Params.Reason,
			},
		}
		resp.FrameType = onebot.Frame_TSetGroupAddRequestResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupAddRequestReq); !resp.Ok {
			return
		}
		r := HandleSetGroupAddRequest(cli, reqData.SetGroupAddRequestReq)
		data := &actionResp{
			Status:  "ok",
			RetCode: 0,
			Data:    &r,
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	} else {
		data := &actionResp{
			Status:  "failure",
			RetCode: -1,
			Data:    fmt.Sprintf("请求 %s 失败，%s 不存在或未实现", req.Action, req.Action),
			Echo:    req.Echo,
		}
		sendForwardActionRespData(data, plugin, ws)
	}
}

func HandleEventFrame(cli *client.QQClient, eventFrame *onebot.Frame) {
	go ForwrdSendMsg(cli, eventFrame)
	eventFrame.Ok = true
	eventFrame.BotId = int64(cli.Uin)
	var eventBytes []byte
	if wsprotocol == 1 {
		var err error
		eventBytes, err = json.Marshal(eventFrame)
		if err != nil {
			log.Errorf("event 序列化错误 %v", err)
			return
		}
	} else {
		var err error
		eventBytes, err = proto.Marshal(eventFrame)
		if err != nil {
			log.Errorf("event 序列化错误 %v", err)
			return
		}
	}

	wsServers, ok := RemoteServers.Load(int64(cli.Uin))
	if !ok {
		log.Warnf("failed to load remote servers, %+v", fmt.Sprintf("%v", cli.Uin))
		return
	}

	for _, ws := range wsServers {
		if ws.EventFilter != nil && len(ws.EventFilter) > 0 { // 有event filter
			if !int32SliceContains(ws.EventFilter, int32(eventFrame.FrameType)) {
				log.Debugf("EventFilter 跳过 [%s](%s)", ws.Name, ws.wsUrl)
				continue
			}
		}
		if wsprotocol == 1 {
			err := json.Unmarshal(eventBytes, eventFrame) // 每个serverGroup, eventFrame 恢复原消息，防止因正则匹配互相影响
			if err != nil {
				log.Errorf("failed to unmarshal raw event frame, %+v", err)
				return
			}
		} else {
			err := proto.Unmarshal(eventBytes, eventFrame) // 每个serverGroup, eventFrame 恢复原消息，防止因正则匹配互相影响
			if err != nil {
				log.Errorf("failed to unmarshal raw event frame, %+v", err)
				return
			}
		}

		report := true // 是否上报event

		if ws.regexp != nil { // 有prefix filter
			if e, ok := eventFrame.PbData.(*onebot.Frame_PrivateMessageEvent); ok {
				if report = ws.regexp.MatchString(e.PrivateMessageEvent.RawMessage); report && ws.RegexReplace != "" {
					e.PrivateMessageEvent.RawMessage = ws.regexp.ReplaceAllString(e.PrivateMessageEvent.RawMessage, ws.RegexReplace)
				}
			}
			if e, ok := eventFrame.PbData.(*onebot.Frame_GroupMessageEvent); ok {
				if report = ws.regexp.MatchString(e.GroupMessageEvent.RawMessage); report && ws.RegexReplace != "" {
					e.GroupMessageEvent.RawMessage = ws.regexp.ReplaceAllString(e.GroupMessageEvent.RawMessage, ws.RegexReplace)
				}
			}
		}

		if report {
			// 使用json上报
			if pme, ok := eventFrame.PbData.(*onebot.Frame_PrivateMessageEvent); ok {
				sendingString, err := json.Marshal(pme.PrivateMessageEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gme, ok := eventFrame.PbData.(*onebot.Frame_GroupMessageEvent); ok {
				sendingString, err := json.Marshal(gme.GroupMessageEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if fan, ok := eventFrame.PbData.(*onebot.Frame_FriendAddNoticeEvent); ok {
				sendingString, err := json.Marshal(fan.FriendAddNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if frn, ok := eventFrame.PbData.(*onebot.Frame_FriendRecallNoticeEvent); ok {
				sendingString, err := json.Marshal(frn.FriendRecallNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if fr, ok := eventFrame.PbData.(*onebot.Frame_FriendRequestEvent); ok {
				sendingString, err := json.Marshal(fr.FriendRequestEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gan, ok := eventFrame.PbData.(*onebot.Frame_GroupAdminNoticeEvent); ok {
				sendingString, err := json.Marshal(gan.GroupAdminNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gbn, ok := eventFrame.PbData.(*onebot.Frame_GroupBanNoticeEvent); ok {
				sendingString, err := json.Marshal(gbn.GroupBanNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gdn, ok := eventFrame.PbData.(*onebot.Frame_GroupDecreaseNoticeEvent); ok {
				sendingString, err := json.Marshal(gdn.GroupDecreaseNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gin, ok := eventFrame.PbData.(*onebot.Frame_GroupIncreaseNoticeEvent); ok {
				sendingString, err := json.Marshal(gin.GroupIncreaseNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gn, ok := eventFrame.PbData.(*onebot.Frame_GroupNotifyEvent); ok {
				sendingString, err := json.Marshal(gn.GroupNotifyEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if grn, ok := eventFrame.PbData.(*onebot.Frame_GroupRecallNoticeEvent); ok {
				sendingString, err := json.Marshal(grn.GroupRecallNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gr, ok := eventFrame.PbData.(*onebot.Frame_GroupRequestEvent); ok {
				sendingString, err := json.Marshal(gr.GroupRequestEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gtm, ok := eventFrame.PbData.(*onebot.Frame_GroupTempMessageEvent); ok {
				sendingString, err := json.Marshal(gtm.GroupTempMessageEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gun, ok := eventFrame.PbData.(*onebot.Frame_GroupUploadNoticeEvent); ok {
				sendingString, err := json.Marshal(gun.GroupUploadNoticeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if p, ok := eventFrame.PbData.(*onebot.Frame_GroupPokeEvent); ok {
				sendingString, err := json.Marshal(p.GroupPokeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if p, ok := eventFrame.PbData.(*onebot.Frame_FriendPokeEvent); ok {
				sendingString, err := json.Marshal(p.FriendPokeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gd, ok := eventFrame.PbData.(*onebot.Frame_GroupDigestEvent); ok {
				sendingString, err := json.Marshal(gd.GroupDigestEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gmpc, ok := eventFrame.PbData.(*onebot.Frame_GroupMemberPermissionChangeEvent); ok {
				sendingString, err := json.Marshal(gmpc.GroupMemberPermissionChangeEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if gn, ok := eventFrame.PbData.(*onebot.Frame_GroupNameUpdatedEvent); ok {
				sendingString, err := json.Marshal(gn.GroupNameUpdatedEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if mtc, ok := eventFrame.PbData.(*onebot.Frame_MemberSpecialTitleUpdatedEvent); ok {
				sendingString, err := json.Marshal(mtc.MemberSpecialTitleUpdatedEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
			if r, ok := eventFrame.PbData.(*onebot.Frame_RenameEvent); ok {
				sendingString, err := json.Marshal(r.RenameEvent)
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				if ws.Json {
					_ = ws.Send(websocket.TextMessage, sendingString)
				} else {
					_ = ws.Send(websocket.BinaryMessage, sendingString)
				}
			}
		}
	}
}

func int32SliceContains(numbers []int32, num int32) bool {
	for _, number := range numbers {
		if number == num {
			return true
		}
	}
	return false
}

func sendActionRespData(ar *actionResp, plugin *config.Plugin, ws *safe_ws.SafeWebSocket) {
	data, err := json.Marshal(ar)
	if err == nil {
		if plugin.Json {
			_ = ws.Send(websocket.TextMessage, data)
		} else {
			_ = ws.Send(websocket.BinaryMessage, data)
		}
	}
}

func sendForwardActionRespData(ar *actionResp, plugin *config.Plugin, ws *safe_ws.ForwardSafeWebSocket) {
	data, err := json.Marshal(ar)
	if err == nil {
		if plugin.Json {
			ws.ForwardSend(websocket.TextMessage, data)
		} else {
			ws.ForwardSend(websocket.TextMessage, data)
		}
	}
}
