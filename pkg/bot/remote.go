package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/2mf8/Go-Lagrange-Client/pkg/config"
	"github.com/2mf8/Go-Lagrange-Client/pkg/safe_ws"
	"github.com/2mf8/Go-Lagrange-Client/pkg/util"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/onebot"

	"github.com/2mf8/LagrangeGo/client"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
)

//go:generate go run github.com/a8m/syncmap -o "gen_remote_map.go" -pkg bot -name RemoteMap "map[int64]map[string]*WsServer"
var (
	// RemoteServers key是botId，value是map（key是serverName，value是server）
	RemoteServers RemoteMap
	jsonMarshaler = jsonpb.Marshaler{
		OrigName:     true,
		EmitDefaults: false,
	}
	jsonUnmarshaler = jsonpb.Unmarshaler{
		AllowUnknownFields: true,
	}
)

type WsServer struct {
	*safe_ws.SafeWebSocket        // 线程安全的ws
	*config.Plugin                // 服务器组配置
	wsUrl                  string // 随机抽中的url
	regexp                 *regexp.Regexp
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
		var apiReq onebot.Frame
		switch messageType {
		case websocket.BinaryMessage:
			err := proto.Unmarshal(data, &apiReq)
			if err != nil {
				log.Errorf("收到API binary，解析错误 %v", err)
				return
			}
		case websocket.TextMessage:
			err := jsonUnmarshaler.Unmarshal(bytes.NewReader(data), &apiReq)
			if err != nil {
				log.Errorf("收到API text，解析错误 %v", err)
				return
			}
		}

		log.Debugf("收到 apiReq 信息, %+v", util.MustMarshal(apiReq))

		apiResp := handleApiFrame(cli, &apiReq, isApiAllow)
		var (
			respBytes []byte
			err       error
		)
		switch messageType {
		case websocket.BinaryMessage:
			respBytes, err = proto.Marshal(apiResp)
			if err != nil {
				log.Errorf("failed to marshal api resp, %+v", err)
			}
		case websocket.TextMessage:
			respStr, err := jsonMarshaler.MarshalToString(apiResp)
			if err != nil {
				log.Errorf("failed to marshal api resp, %+v", err)
			}
			respBytes = []byte(respStr)
		}
		log.Debugf("发送 apiResp 信息, %+v", util.MustMarshal(apiResp))
		_ = ws.Send(messageType, respBytes)
	}
}

func handleApiFrame(cli *client.QQClient, req *onebot.Frame, isApiAllow func(onebot.Frame_FrameType) bool) (resp *onebot.Frame) {
	resp = &onebot.Frame{
		BotId: int64(cli.Uin),
		Echo:  req.Echo,
		Ok:    true,
	}
	switch data := req.PbData.(type) {
	case *onebot.Frame_SendPrivateMsgReq:
		resp.FrameType = onebot.Frame_TSendPrivateMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendPrivateMsgReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SendPrivateMsgResp{
			SendPrivateMsgResp: HandleSendPrivateMsg(cli, data.SendPrivateMsgReq),
		}
	case *onebot.Frame_SendGroupMsgReq:
		resp.FrameType = onebot.Frame_TSendGroupMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendGroupMsgReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SendGroupMsgResp{
			SendGroupMsgResp: HandleSendGroupMsg(cli, data.SendGroupMsgReq),
		}
	case *onebot.Frame_SendMsgReq:
		resp.FrameType = onebot.Frame_TSendMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TSendMsgReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SendMsgResp{
			SendMsgResp: HandleSendMsg(cli, data.SendMsgReq),
		}
	case *onebot.Frame_GetMsgReq:
		resp.FrameType = onebot.Frame_TGetMsgResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetMsgReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetMsgResp{
			GetMsgResp: HandleGetMsg(cli, data.GetMsgReq),
		}
	case *onebot.Frame_SetGroupWholeBanReq:
		resp.FrameType = onebot.Frame_TSetGroupWholeBanResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupWholeBanReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SetGroupWholeBanResp{
			SetGroupWholeBanResp: HandleSetGroupWholeBan(cli, data.SetGroupWholeBanReq),
		}
	case *onebot.Frame_SetGroupCardReq:
		resp.FrameType = onebot.Frame_TSetGroupCardResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupCardReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SetGroupCardResp{
			SetGroupCardResp: HandleSetGroupCard(cli, data.SetGroupCardReq),
		}
	case *onebot.Frame_SetGroupNameReq:
		resp.FrameType = onebot.Frame_TSetGroupNameResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupNameReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SetGroupNameResp{
			SetGroupNameResp: HandleSetGroupName(cli, data.SetGroupNameReq),
		}
	case *onebot.Frame_SetGroupLeaveReq:
		resp.FrameType = onebot.Frame_TSetGroupLeaveResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupLeaveReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SetGroupLeaveResp{
			SetGroupLeaveResp: HandleSetGroupLeave(cli, data.SetGroupLeaveReq),
		}
	case *onebot.Frame_SetGroupSpecialTitleReq:
		resp.FrameType = onebot.Frame_TSetGroupSpecialTitleResp
		if resp.Ok = isApiAllow(onebot.Frame_TSetGroupSpecialTitleReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_SetGroupSpecialTitleResp{
			SetGroupSpecialTitleResp: HandleSetGroupSpecialTitle(cli, data.SetGroupSpecialTitleReq),
		}
	/* case *onebot.Frame_SetFriendAddRequestReq:
	resp.FrameType = onebot.Frame_TSetFriendAddRequestResp
	if resp.Ok = isApiAllow(onebot.Frame_TSetFriendAddRequestReq); !resp.Ok {
		return
	}
	resp.PbData = &onebot.Frame_SetFriendAddRequestResp{
		SetFriendAddRequestResp: HandleSetFriendAddRequest(cli, data.SetFriendAddRequestReq),
	} */
	/* case *onebot.Frame_SetGroupAddRequestReq:
	resp.FrameType = onebot.Frame_TSetGroupAddRequestResp
	if resp.Ok = isApiAllow(onebot.Frame_TSetGroupAddRequestReq); !resp.Ok {
		return
	}
	resp.PbData = &onebot.Frame_SetGroupAddRequestResp{
		SetGroupAddRequestResp: HandleSetGroupAddRequest(cli, data.SetGroupAddRequestReq),
	} */
	case *onebot.Frame_GetLoginInfoReq:
		resp.FrameType = onebot.Frame_TGetLoginInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetLoginInfoReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetLoginInfoResp{
			GetLoginInfoResp: HandleGetLoginInfo(cli, data.GetLoginInfoReq),
		}
	case *onebot.Frame_GetFriendListReq:
		resp.FrameType = onebot.Frame_TGetFriendListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetFriendListReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetFriendListResp{
			GetFriendListResp: HandleGetFriendList(cli, data.GetFriendListReq),
		}
	case *onebot.Frame_GetGroupInfoReq:
		resp.FrameType = onebot.Frame_TGetGroupInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupInfoReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetGroupInfoResp{
			GetGroupInfoResp: HandleGetGroupInfo(cli, data.GetGroupInfoReq),
		}
	case *onebot.Frame_GetGroupListReq:
		resp.FrameType = onebot.Frame_TGetGroupListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupListReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetGroupListResp{
			GetGroupListResp: HandleGetGroupList(cli, data.GetGroupListReq),
		}
	case *onebot.Frame_GetGroupMemberInfoReq:
		resp.FrameType = onebot.Frame_TGetGroupMemberInfoResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberInfoReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetGroupMemberInfoResp{
			GetGroupMemberInfoResp: HandleGetGroupMemberInfo(cli, data.GetGroupMemberInfoReq),
		}
	case *onebot.Frame_GetGroupMemberListReq:
		resp.FrameType = onebot.Frame_TGetGroupMemberListResp
		if resp.Ok = isApiAllow(onebot.Frame_TGetGroupMemberListReq); !resp.Ok {
			return
		}
		resp.PbData = &onebot.Frame_GetGroupMemberListResp{
			GetGroupMemberListResp: HandleGetGroupMemberList(cli, data.GetGroupMemberListReq),
		}
	/* case *onebot.Frame_GetStrangerInfoReq:
	resp.FrameType = onebot.Frame_TGetStrangerInfoResp
	if resp.Ok = isApiAllow(onebot.Frame_TGetStrangerInfoReq); !resp.Ok {
		return
	}
	resp.PbData = &onebot.Frame_GetStrangerInfoResp{
		GetStrangerInfoResp: HandleGetStrangerInfo(cli, data.GetStrangerInfoReq),
	} */
	default:
		return resp
	}
	return resp
}

func HandleEventFrame(cli *client.QQClient, eventFrame *onebot.Frame) {
	eventFrame.Ok = true
	eventFrame.BotId = int64(cli.Uin)
	eventBytes, err := proto.Marshal(eventFrame)
	if err != nil {
		log.Errorf("event 序列化错误 %v", err)
		return
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

		err := proto.Unmarshal(eventBytes, eventFrame) // 每个serverGroup, eventFrame 恢复原消息，防止因正则匹配互相影响
		if err != nil {
			log.Errorf("failed to unmarshal raw event frame, %+v", err)
			return
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
			if ws.Json {
				util.IsJson = true
				// 使用json上报
				sendingString, err := json.Marshal(eventFrame.Data)
				fmt.Println(string(sendingString))
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				_ = ws.Send(websocket.TextMessage, sendingString)
			} else {
				// 使用protobuf上报
				sendingBytes, err := proto.Marshal(eventFrame) // 使用正则修改后的eventFrame
				if err != nil {
					log.Errorf("event 序列化错误 %v", err)
					continue
				}
				log.Debugf("上报 event 给 [%s](%s)", ws.Name, ws.wsUrl)
				_ = ws.Send(websocket.BinaryMessage, sendingBytes)
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
