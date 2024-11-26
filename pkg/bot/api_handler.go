package bot

import (
	"encoding/json"
	"fmt"
	_ "image/gif" // 用于解决发不出图片的问题
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	_ "unsafe"

	"github.com/2mf8/Go-Lagrange-Client/pkg/bot/clz"
	"github.com/2mf8/Go-Lagrange-Client/pkg/cache"
	"github.com/2mf8/Go-Lagrange-Client/pkg/config"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/onebot"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/client/entity"
	"github.com/LagrangeDev/LagrangeGo/message"
	log "github.com/sirupsen/logrus"
)

const MAX_TEXT_LENGTH = 80

type ForwardNode struct {
	Type string            `json:"type,omitempty"`
	Data *ForwardChildNode `json:"data,omitempty"`
}

type ForwardChildNode struct {
	Uin     int64             `json:"uin,omitempty"`
	Name    string            `json:"name,omitempty"`
	GroupId int64             `json:"group_id,omitempty"`
	Content []*onebot.Message `json:"content,omitempty"`
}

// 风控临时解决方案
func splitText(content string, limit int) []string {
	text := []rune(content)

	result := make([]string, 0)
	num := int(math.Ceil(float64(len(text)) / float64(limit)))
	for i := 0; i < num; i++ {
		start := i * limit
		end := func() int {
			if (i+1)*limit > len(text) {
				return len(text)
			} else {
				return (i + 1) * limit
			}
		}()
		result = append(result, string(text[start:end]))
	}
	return result
}

func preprocessImageMessage(cli *client.QQClient, groupUin uint32, path string) (string, *message.ImageElement, error) {
	if strings.Contains(path, "http") {
		resp, err := http.Get(path)
		defer resp.Body.Close()
		if err != nil {
			return "", nil, err
		}
		imo, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", nil, err
		}
		filename := fmt.Sprintf("%v.png", time.Now().UnixMicro())
		err = os.WriteFile(filename, imo, 0666)
		if err != nil {
			return "", nil, err
		}
		f, err := os.Open(filename)
		if err != nil {
			return "", nil, err
		}
		defer func() { _ = f.Close() }()
		ir, err := cli.ImageUploadGroup(groupUin, message.NewStreamImage(f))
		if err != nil {
			return "", nil, err
		}
		return filename, ir, nil
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", nil, err
		}
		defer func() { _ = f.Close() }()
		ir, err := cli.ImageUploadGroup(groupUin, message.NewStreamImage(f))
		if err != nil {
			return "", nil, err
		}
		return "", ir, nil
	}
}

func preprocessVideoMessage(cli *client.QQClient, groupUin uint32, video *clz.LocalVideo) (*message.ShortVideoElement, error) {
	v, err := os.Open(video.File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = v.Close() }()
	return cli.VideoUploadGroup(groupUin, message.NewStreamVideo(v, v))
}
func preprocessVideoMessagePrivate(cli *client.QQClient, targetUid string, video *clz.LocalVideo) (*message.ShortVideoElement, error) {
	v, err := os.Open(video.File)
	if err != nil {
		return nil, err
	}
	defer func() { _ = v.Close() }()
	return cli.VideoUploadPrivate(targetUid, message.NewStreamVideo(v, v))
}

func preprocessImageMessagePrivate(cli *client.QQClient, targetUid string, path string) (string, *message.ImageElement, error) {
	if strings.Contains(path, "http") {
		resp, err := http.Get(path)
		defer resp.Body.Close()
		if err != nil {
			return "", nil, err
		}
		imo, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", nil, err
		}
		filename := fmt.Sprintf("%v.png", time.Now().UnixMicro())
		err = os.WriteFile(filename, imo, 0666)
		if err != nil {
			return "", nil, err
		}
		f, err := os.Open(filename)
		if err != nil {
			return "", nil, err
		}
		defer func() { _ = f.Close() }()
		ir, err := cli.ImageUploadPrivate(targetUid, message.NewStreamImage(f))
		if err != nil {
			return "", nil, err
		}
		return filename, ir, nil
	} else {
		f, err := os.Open(path)
		if err != nil {
			return "", nil, err
		}
		defer func() { _ = f.Close() }()
		ir, err := cli.ImageUploadPrivate(targetUid, message.NewStreamImage(f))
		if err != nil {
			return "", nil, err
		}
		return "", ir, nil
	}
}

func HandleSendPrivateMsg(cli *client.QQClient, req *onebot.SendPrivateMsgReq) *onebot.SendPrivateMsgResp {
	tUid := cli.GetUID(uint32(req.UserId))
	messageChain := make([]message.IMessageElement, 0)
	miraiMsg := ProtoMsgToMiraiMsg(cli, req.Message, req.AutoEscape)
	for _, v := range miraiMsg {
		if v.Type() == message.Image {
			t, ok := v.(*message.ImageElement)
			if ok {
				fn, elem, err := preprocessImageMessagePrivate(cli, tUid, t.URL)
				if fn != "" {
					os.Remove(fn)
				}
				if err == nil {
					messageChain = append(messageChain, elem)
				}
			}
		} else if v.Type() == message.Video {
			iv, ok := v.(*clz.LocalVideo)
			if ok {
				elem, err := preprocessVideoMessagePrivate(cli, tUid, iv)
				fmt.Println(err)
				if err == nil {
					messageChain = append(messageChain, elem)
				}
			}
		} else {
			messageChain = append(messageChain, v)
		}
	}
	sendingMessage := &message.SendingMessage{Elements: messageChain}
	log.Infof("Bot(%d) Private(%d) <- %s", cli.Uin, req.UserId, MiraiMsgToRawMsg(cli, miraiMsg))
	ret, _ := cli.SendPrivateMessage(uint32(req.UserId), sendingMessage.Elements)
	cache.PrivateMessageLru.Add(ret.ID, ret)
	return &onebot.SendPrivateMsgResp{
		MessageId: int32(ret.ID),
	}
}

func HandleSendGroupMsg(cli *client.QQClient, req *onebot.SendGroupMsgReq) *onebot.SendGroupMsgResp {
	messageChain := make([]message.IMessageElement, 0)
	if g := cli.GetCachedGroupInfo(uint32(req.GroupId)); g == nil {
		log.Warnf("发送消息失败，群聊 %d 不存在", req.GroupId)
		return nil
	}
	miraiMsg := ProtoMsgToMiraiMsg(cli, req.Message, req.AutoEscape)
	for _, v := range miraiMsg {
		if v.Type() == message.Image {
			t, ok := v.(*message.ImageElement)
			if ok {
				fn, elem, err := preprocessImageMessage(cli, uint32(req.GroupId), t.URL)
				if fn != "" {
					os.Remove(fn)
				}
				if err == nil {
					messageChain = append(messageChain, elem)
				}
			}
		} else if v.Type() == message.Video {
			iv, ok := v.(*clz.LocalVideo)
			if ok {
				elem, err := preprocessVideoMessage(cli, uint32(req.GroupId), iv)
				fmt.Println(err)
				if err == nil {
					messageChain = append(messageChain, elem)
				}
			}
		} else {
			messageChain = append(messageChain, v)
		}
	}
	sendingMessage := &message.SendingMessage{Elements: messageChain}
	log.Infof("Bot(%d) Group(%d) <- %s", cli.Uin, req.GroupId, MiraiMsgToRawMsg(cli, miraiMsg))
	if len(sendingMessage.Elements) == 0 {
		log.Warnf("发送消息内容为空")
		return nil
	}
	ret, err := cli.SendGroupMessage(uint32(req.GroupId), sendingMessage.Elements)
	fmt.Println(err)
	if ret.ID < 1 {
		config.Fragment = !config.Fragment
		log.Warnf("发送群消息失败，可能被风控，下次发送将改变分片策略，Fragment: %+v", config.Fragment)
		return nil
	}
	cache.GroupMessageLru.Add(ret.ID, ret)
	return &onebot.SendGroupMsgResp{
		MessageId: int32(ret.ID),
	}
}

func HandleSendForwardMsg(cli *client.QQClient, req *onebot.SendForwardMsgReq) *onebot.SendForwardMsgResp {
	ms := []*ForwardNode{}
	db, err := json.Marshal(req.Messages)
	log.Warn(string(db), err)
	if err != nil {
		return nil
	}
	err = json.Unmarshal(db, &ms)
	if err != nil {
		return nil
	}
	nodes := []*message.ForwardNode{}
	for _, v := range ms {
		tUid := cli.GetUID(uint32(v.Data.Uin))
		messageChain := make([]message.IMessageElement, 0)
		miraiMsg := ProtoMsgToMiraiMsg(cli, v.Data.Content, false)
		for _, iv := range miraiMsg {
			if iv.Type() == message.Image {
				t, ok := iv.(*message.ImageElement)
				if ok {
					if req.GroupId > 0 && v.Data.GroupId > 0 {
						fn, elem, err := preprocessImageMessage(cli, uint32(v.Data.GroupId), t.URL)
						if fn != "" {
							os.Remove(fn)
						}
						if err == nil {
							messageChain = append(messageChain, elem)
						}
					} else {
						fn, elem, err := preprocessImageMessagePrivate(cli, tUid, t.URL)
						if fn != "" {
							os.Remove(fn)
						}
						if err == nil {
							messageChain = append(messageChain, elem)
						}
					}
				}
			} else {
				messageChain = append(messageChain, iv)
			}
		}
		sendingMessage := &message.SendingMessage{Elements: messageChain}
		node := &message.ForwardNode{
			GroupID:    uint32(v.Data.GroupId),
			SenderID:   uint32(v.Data.Uin),
			SenderName: v.Data.Name,
			Time:       uint32(time.Now().Unix()),
			Message:    sendingMessage.Elements,
		}
		nodes = append(nodes, node)
	}
	ifm := &message.ForwardMessage{
		Nodes: nodes,
	}
	fmt.Println(req.GroupId)
	fm, err := cli.UploadForwardMsg(ifm, uint32(req.GroupId))
	if err != nil {
		log.Warn("发送合并转发消息失败")
	}
	fml, _ := json.Marshal(fm)
	fmt.Println(string(fml))
	return &onebot.SendForwardMsgResp{
		ResId: fm.ResID,
	}
}

func HandleSendMsg(cli *client.QQClient, req *onebot.SendMsgReq) *onebot.SendMsgResp {
	miraiMsg := ProtoMsgToMiraiMsg(cli, req.Message, req.AutoEscape)
	sendingMessage := &message.SendingMessage{Elements: miraiMsg}

	if req.GroupId != 0 && req.UserId != 0 { // 临时
		ret, _ := cli.SendTempMessage(uint32(req.GroupId), uint32(req.UserId), sendingMessage.Elements)
		cache.PrivateMessageLru.Add(ret.ID, ret)
		return &onebot.SendMsgResp{
			MessageId: int32(ret.ID),
		}
	}

	if req.GroupId != 0 { // 群
		if g := cli.GetCachedGroupInfo(uint32(req.GroupId)); g == nil {
			log.Warnf("发送消息失败，群聊 %d 不存在", req.GroupId)
			return nil
		}
		ret, _ := cli.SendGroupMessage(uint32(req.GroupId), sendingMessage.Elements)
		if ret.ID < 1 {
			config.Fragment = !config.Fragment
			log.Warnf("发送群消息失败，可能被风控，下次发送将改变分片策略，Fragment: %+v", config.Fragment)
			return nil
		}
		cache.GroupMessageLru.Add(ret.ID, ret)
		return &onebot.SendMsgResp{
			MessageId: int32(ret.ID),
		}
	}

	if req.UserId != 0 { // 私聊
		ret, _ := cli.SendPrivateMessage(uint32(req.UserId), sendingMessage.Elements)
		cache.PrivateMessageLru.Add(ret.ID, ret)
		return &onebot.SendMsgResp{
			MessageId: int32(ret.ID),
		}
	}
	log.Warnf("failed to send msg")
	return nil
}

func HandleGetMsg(cli *client.QQClient, req *onebot.GetMsgReq) *onebot.GetMsgResp {
	eventInterface, isGroup := cache.GroupMessageLru.Get(req.MessageId)
	if isGroup {
		event := eventInterface.(*message.GroupMessage)
		messageType := "group"
		if event.Sender.Uin == cli.Uin {
			messageType = "self"
		}
		return &onebot.GetMsgResp{
			Time:        int32(event.Time),
			MessageType: messageType,
			MessageId:   req.MessageId,
			RealId:      int32(event.InternalID), // 不知道是什么？
			Message:     MiraiMsgToProtoMsg(cli, event.Elements),
			RawMessage:  MiraiMsgToRawMsg(cli, event.Elements),
			Sender: &onebot.GetMsgResp_Sender{
				UserId:   int64(event.Sender.Uin),
				Nickname: event.Sender.Nickname,
			},
		}

	}
	eventInterface, isPrivate := cache.PrivateMessageLru.Get(req.MessageId)
	if isPrivate {
		event := eventInterface.(*message.PrivateMessage)
		messageType := "private"
		if event.Sender.Uin == cli.Uin {
			messageType = "self"
		}
		return &onebot.GetMsgResp{
			Time:        int32(event.Time),
			MessageType: messageType,
			MessageId:   req.MessageId,
			RealId:      int32(event.InternalID), // 不知道是什么？
			Message:     MiraiMsgToProtoMsg(cli, event.Elements),
			RawMessage:  MiraiMsgToRawMsg(cli, event.Elements),
			Sender: &onebot.GetMsgResp_Sender{
				UserId:   int64(event.Sender.Uin),
				Nickname: event.Sender.Nickname,
			},
		}
	}
	return nil
}

func HandleDeletMsg(cli *client.QQClient, req *onebot.DeleteMsgReq) *onebot.DeleteMsgResp {
	if eventInterface, ok := cache.GroupMessageLru.Get(req.MessageId); ok {
		if event, ok := eventInterface.(*message.GroupMessage); ok {
			if err := cli.RecallGroupMessage(event.GroupUin, uint32(event.ID)); err != nil {
				return &onebot.DeleteMsgResp{}
			}
		}
	}
	return nil
}

func ReleaseClient(cli *client.QQClient) {
	cli.Release()
	if wsServers, ok := RemoteServers.Load(int64(cli.Uin)); ok {
		for _, wsServer := range wsServers {
			wsServer.Close()
		}
	}
	RemoteServers.Delete(int64(cli.Uin))
}

func HandleSetGroupKick(cli *client.QQClient, req *onebot.SetGroupKickReq) *onebot.SetGroupKickResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.UserId), group.GroupUin); member != nil {
			if err := cli.GroupKickMember(group.GroupUin, member.Uin, req.RejectAddRequest); err != nil {
				return nil
			}
			return &onebot.SetGroupKickResp{}
		}
	}
	return nil
}

func HandleSetGroupBan(cli *client.QQClient, req *onebot.SetGroupBanReq) *onebot.SetGroupBanResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.UserId), group.GroupUin); member != nil {
			if err := cli.GroupMuteMember(group.GroupUin, member.Uin, uint32(req.Duration)); err != nil {
				return nil
			}
			return &onebot.SetGroupBanResp{}
		}
	}
	return nil
}

func HandleSetGroupWholeBan(cli *client.QQClient, req *onebot.SetGroupWholeBanReq) *onebot.SetGroupWholeBanResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		cli.GroupMuteGlobal(group.GroupUin, req.Enable)
		return &onebot.SetGroupWholeBanResp{}
	}
	return nil
}

func HandleSetGroupCard(cli *client.QQClient, req *onebot.SetGroupCardReq) *onebot.SetGroupCardResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.UserId), group.GroupUin); member != nil {
			cli.GroupRenameMember(group.GroupUin, member.Uin, req.Card)
			return &onebot.SetGroupCardResp{}
		}
	}
	return nil
}

func HandleSetGroupName(cli *client.QQClient, req *onebot.SetGroupNameReq) *onebot.SetGroupNameResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		cli.GroupRename(group.GroupUin, req.GroupName)
		return &onebot.SetGroupNameResp{}
	}
	return nil
}

func HandleSetGroupLeave(cli *client.QQClient, req *onebot.SetGroupLeaveReq) *onebot.SetGroupLeaveResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		cli.GroupLeave(group.GroupUin)
		return &onebot.SetGroupLeaveResp{}
	}
	return nil
}

func HandleSetGroupSpecialTitle(cli *client.QQClient, req *onebot.SetGroupSpecialTitleReq) *onebot.SetGroupSpecialTitleResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.UserId), group.GroupUin); member != nil {
			cli.GroupSetSpecialTitle(group.GroupUin, member.Uin, req.SpecialTitle)
			return &onebot.SetGroupSpecialTitleResp{}
		}
	}
	return nil
}

func HandleGetLoginInfo(cli *client.QQClient, req *onebot.GetLoginInfoReq) *onebot.GetLoginInfoResp {
	return &onebot.GetLoginInfoResp{
		UserId:   int64(cli.Uin),
		Nickname: cli.NickName(),
	}
}

func HandleGetFriendList(cli *client.QQClient, req *onebot.GetFriendListReq) *onebot.GetFriendListResp {
	friendList := make([]*onebot.GetFriendListResp_Friend, 0)
	friends, _ := cli.GetFriendsData()
	for _, friend := range friends {
		friendList = append(friendList, &onebot.GetFriendListResp_Friend{
			UserId:   int64(friend.Uin),
			Nickname: friend.Nickname,
			Remark:   friend.Remarks,
		})
	}
	return &onebot.GetFriendListResp{
		Friend: friendList,
	}
}

func HandleGetGroupInfo(cli *client.QQClient, req *onebot.GetGroupInfoReq) *onebot.GetGroupInfoResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		return &onebot.GetGroupInfoResp{
			GroupId:        int64(group.GroupUin),
			GroupName:      group.GroupName,
			MaxMemberCount: int32(group.MaxMember),
			MemberCount:    int32(group.MemberCount),
		}
	}
	return nil
}

func HandleGetGroupList(cli *client.QQClient, req *onebot.GetGroupListReq) *onebot.GetGroupListResp {
	groupList := make([]*onebot.GetGroupListResp_Group, 0)
	groups := cli.GetCachedAllGroupsInfo()
	for _, group := range groups {
		groupList = append(groupList, &onebot.GetGroupListResp_Group{
			GroupId:        int64(group.GroupUin),
			GroupName:      group.GroupName,
			MaxMemberCount: int32(group.MaxMember),
			MemberCount:    int32(group.MemberCount),
		})
	}
	return &onebot.GetGroupListResp{
		Group: groupList,
	}
}

func HandleGetGroupMemberInfo(cli *client.QQClient, req *onebot.GetGroupMemberInfoReq) *onebot.GetGroupMemberInfoResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.UserId), group.GroupUin); member != nil {
			return &onebot.GetGroupMemberInfoResp{
				GroupId:      req.GroupId,
				UserId:       req.UserId,
				Nickname:     member.MemberName,
				Card:         member.MemberCard,
				JoinTime:     int64(member.JoinTime),
				LastSentTime: int64(member.LastMsgTime),
				Level:        strconv.FormatInt(int64(member.GroupLevel), 10),
				Role: func() string {
					switch member.Permission {
					case entity.Owner:
						return "owner"
					case entity.Admin:
						return "admin"
					default:
						return "member"
					}
				}(),
			}
		}
	}
	return nil
}

func HandleGetGroupMemberList(cli *client.QQClient, req *onebot.GetGroupMemberListReq) *onebot.GetGroupMemberListResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		members, err := cli.GetGroupMembersData(uint32(group.GroupUin))
		if err != nil {
			log.Errorf("获取群成员列表失败, %v", err)
			return nil
		}
		memberList := make([]*onebot.GetGroupMemberListResp_GroupMember, 0)
		for _, member := range members {
			memberList = append(memberList, &onebot.GetGroupMemberListResp_GroupMember{
				GroupId:      req.GroupId,
				UserId:       int64(member.Uin),
				Nickname:     member.MemberName,
				Card:         member.MemberCard,
				JoinTime:     int64(member.JoinTime),
				LastSentTime: int64(member.LastMsgTime),
				Level:        strconv.FormatInt(int64(member.GroupLevel), 10),
				Role: func() string {
					switch member.Permission {
					case entity.Owner:
						return "owner"
					case entity.Admin:
						return "admin"
					default:
						return "member"
					}
				}(),
			})
		}
		return &onebot.GetGroupMemberListResp{
			GroupMember: memberList,
		}
	}
	return nil
}

//go:linkname GetCookiesWithDomain github.com/Mrs4s/MiraiGo/client.(*QQClient).getCookiesWithDomain
func GetCookiesWithDomain(c *client.QQClient, domain string) string

func HandleGetCookies(cli *client.QQClient, req *onebot.GetCookiesReq) *onebot.GetCookiesResp {
	return &onebot.GetCookiesResp{Cookies: GetCookiesWithDomain(cli, req.Domain)}
}

//go:linkname GetCSRFToken github.com/Mrs4s/MiraiGo/client.(*QQClient).getCSRFToken
func GetCSRFToken(c *client.QQClient) int

func HandleGetCSRFToken(cli *client.QQClient, req *onebot.GetCsrfTokenReq) *onebot.GetCsrfTokenResp {
	return &onebot.GetCsrfTokenResp{
		Token: int32(GetCSRFToken(cli)),
	}
}

func HandleSendGroupPoke(cli *client.QQClient, req *onebot.SendGroupPokeReq) *onebot.SendGroupPokeResp {
	if group := cli.GetCachedGroupInfo(uint32(req.GroupId)); group != nil {
		if member := cli.GetCachedMemberInfo(uint32(req.ToUin), group.GroupUin); member != nil {
			cli.GroupPoke(group.GroupUin, member.Uin)
		}
	}
	return nil
}

func HandleSendFriendPoke(cli *client.QQClient, req *onebot.SendFriendPokeReq) *onebot.SendFriendPokeResp {
	friends, _ := cli.GetFriendsData()
	for _, friend := range friends {
		if friend.Uin == uint32(req.ToUin) && friend.Uin != cli.Uin {
			cli.FriendPoke(friend.Uin)
		}
	}
	return nil
}

func HandleSetFriendAddRequest(cli *client.QQClient, req *onebot.SetFriendAddRequestReq) *onebot.SetFriendAddRequestResp {
	cli.SetFriendRequest(req.Approve, req.Flag)
	return nil
}

// accept bool, sequence uint64, typ uint32, groupUin uint32, message string
func HandleSetGroupAddRequest(cli *client.QQClient, req *onebot.SetGroupAddRequestReq) *onebot.SetGroupAddRequestResp {
	gid, err := strconv.ParseInt(req.Flag, 10, 64)
	if err != nil {
		log.Warnf("获取群系统消息失败：%v", err)
		return nil
	}
	msgs, err := cli.GetGroupSystemMessages(false, 20, uint32(gid))
	if err != nil {
		log.Warnf("获取群系统消息失败：%v", err)
		return nil
	}
	if req.SubType == "invite" {
		for _, ireq := range msgs.InvitedRequests {
			if req.Flag == fmt.Sprintf("%v", ireq.Sequence) {
				if ireq.Checked() {
					log.Warnf("处理群系统消息失败: 无法操作已处理的消息.")
					return nil
				}
			}
			if req.Approve {
				cli.SetGroupRequest(false, true, ireq.Sequence, uint32(ireq.EventType), ireq.GroupUin, "")
				return nil
			} else {
				cli.SetGroupRequest(false, false, ireq.Sequence, uint32(ireq.EventType), ireq.GroupUin, req.Reason)
				return nil
			}
		}
	} else {
		for _, ireq := range msgs.JoinRequests {
			if req.Flag == fmt.Sprintf("%v", ireq.Sequence) {
				if ireq.Checked() {
					log.Warnf("处理群系统消息失败: 无法操作已处理的消息.")
					return nil
				}
			}
			if req.Approve {
				cli.SetGroupRequest(false, true, ireq.Sequence, uint32(ireq.EventType), ireq.GroupUin, "")
				return nil
			} else {
				cli.SetGroupRequest(false, false, ireq.Sequence, uint32(ireq.EventType), ireq.GroupUin, req.Reason)
				return nil
			}
		}
	}
	log.Warnf("处理群系统消息失败: 消息 %v 不存在.", req.Flag)
	return nil
}
