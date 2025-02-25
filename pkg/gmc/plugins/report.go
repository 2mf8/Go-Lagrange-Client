package plugins

import (
	"fmt"
	"strconv"
	"time"

	"github.com/2mf8/Go-Lagrange-Client/pkg/bot"
	"github.com/2mf8/Go-Lagrange-Client/pkg/cache"
	"github.com/2mf8/Go-Lagrange-Client/pkg/plugin"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/onebot"
	log "github.com/sirupsen/logrus"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/client/event"
	"github.com/LagrangeDev/LagrangeGo/message"
)

func ReportPrivateMessage(cli *client.QQClient, event *message.PrivateMessage) int32 {
	cache.PrivateMessageLru.Add(event.ID, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TPrivateMessageEvent,
	}
	eventProto.Data = &onebot.Frame_PrivateMessageEvent{
		PrivateMessageEvent: &onebot.PrivateMessageEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "message",
			MessageType: "private",
			SubType:     "normal",
			MessageId:   int32(event.ID),
			UserId:      int64(event.Sender.Uin),
			Message:     bot.MiraiMsgToProtoMsg(cli, event.Elements),
			RawMessage:  bot.MiraiMsgToRawMsg(cli, event.Elements),
			Sender: &onebot.PrivateMessageEvent_Sender{
				UserId:   int64(event.Sender.Uin),
				Nickname: event.Sender.Nickname,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupMessage(cli *client.QQClient, event *message.GroupMessage) int32 {
	cache.GroupMessageLru.Add(event.ID, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupMessageEvent,
	}
	groupMessageEvent := &onebot.GroupMessageEvent{
		Time:        time.Now().Unix(),
		SelfId:      int64(cli.Uin),
		PostType:    "message",
		MessageType: "group",
		SubType:     "normal",
		MessageId:   int32(event.ID),
		GroupId:     int64(event.GroupUin),
		UserId:      int64(event.Sender.Uin),
		Message:     bot.MiraiMsgToProtoMsg(cli, event.Elements),
		RawMessage:  bot.MiraiMsgToRawMsg(cli, event.Elements),
		Sender: &onebot.GroupMessageEvent_Sender{
			UserId:   int64(event.Sender.Uin),
			Nickname: event.Sender.Nickname,
			Card:     event.Sender.CardName,
		},
	}

	eventProto.Data = &onebot.Frame_GroupMessageEvent{
		GroupMessageEvent: groupMessageEvent,
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportMemberJoin(cli *client.QQClient, event *event.GroupMemberIncrease) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupIncreaseNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_GroupIncreaseNoticeEvent{
		GroupIncreaseNoticeEvent: &onebot.GroupIncreaseNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "message",
			NoticeType: "group_increase",
			SubType:    "approve",
			GroupId:    int64(event.GroupUin),
			UserId:     0,
			OperatorId: 0,
			Extra: map[string]string{
				"member_uid":  event.UserUID,
				"invitor_uid": event.InvitorUID,
				"join_type":   fmt.Sprintf("%v", event.JoinType),
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportMemberLeave(cli *client.QQClient, event *event.GroupMemberDecrease) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupDecreaseNoticeEvent,
	}
	subType := "leave"
	var operatorUid string = ""
	if event.IsKicked() {
		subType = "kick"
		operatorUid = event.OperatorUID
	}

	eventProto.Data = &onebot.Frame_GroupDecreaseNoticeEvent{
		GroupDecreaseNoticeEvent: &onebot.GroupDecreaseNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "message",
			NoticeType: "group_decrease",
			SubType:    subType,
			GroupId:    int64(event.GroupUin),
			Extra: map[string]string{
				"member_uid":   event.UserUID,
				"operator_uid": operatorUid,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportJoinGroup(cli *client.QQClient, event *event.GroupMemberIncrease) int32 {
	oid := cli.GetUin(event.InvitorUID)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupIncreaseNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_GroupIncreaseNoticeEvent{
		GroupIncreaseNoticeEvent: &onebot.GroupIncreaseNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "message",
			NoticeType: "group_increase",
			SubType:    "approve",
			GroupId:    int64(event.GroupUin),
			UserId:     int64(cli.Uin),
			OperatorId: int64(oid),
			Extra: map[string]string{
				"member_uid":  event.UserUID,
				"join_type":   fmt.Sprintf("%v", event.JoinType),
				"invitor_uid": event.InvitorUID,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupMute(cli *client.QQClient, event *event.GroupMute) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupBanNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_GroupBanNoticeEvent{
		GroupBanNoticeEvent: &onebot.GroupBanNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "group_ban",
			SubType: func() string {
				if event.Duration == 0 {
					return "lift_ban"
				}
				return "ban"
			}(),
			GroupId:  int64(event.GroupUin),
			Duration: int64(event.Duration),
			Extra: map[string]string{
				"operator_uid": event.OperatorUID,
				"target_uid":   event.UserUID,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportNewFriendRequest(cli *client.QQClient, event *event.NewFriendRequest) int32 {
	flag := event.SourceUID
	cache.FriendRequestLru.Add(flag, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendRequestEvent,
	}
	eventProto.Data = &onebot.Frame_FriendRequestEvent{
		FriendRequestEvent: &onebot.FriendRequestEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "request",
			RequestType: "friend",
			Flag:        flag,
			Extra: map[string]string{
				"source_uid": event.SourceUID,
				"msg":        event.Msg,
				"source":     event.Source,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportUserJoinGroupRequest(cli *client.QQClient, event *event.GroupMemberJoinRequest) int32 {
	flag := strconv.FormatInt(int64(event.GroupUin), 10)
	cache.GroupRequestLru.Add(flag, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupRequestEvent,
	}
	eventProto.Data = &onebot.Frame_GroupRequestEvent{
		GroupRequestEvent: &onebot.GroupRequestEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "request",
			RequestType: "group",
			SubType:     "add",
			GroupId:     int64(event.GroupUin),
			Flag:        flag,
			Extra: map[string]string{
				"target_uid":  event.UserUID,
				"invitor_uid": event.InvitorUID,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupInvitedRequest(cli *client.QQClient, event *event.GroupInvite) int32 {
	flag := strconv.FormatInt(int64(event.RequestSeq), 10)
	uin := cli.GetUin(event.InvitorUID)
	cache.GroupInvitedRequestLru.Add(flag, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupRequestEvent,
	}
	eventProto.Data = &onebot.Frame_GroupRequestEvent{
		GroupRequestEvent: &onebot.GroupRequestEvent{
			Time:          time.Now().Unix(),
			SelfId:        int64(cli.Uin),
			PostType:      "request",
			RequestType:   "group",
			SubType:       "invite",
			GroupId:       int64(event.GroupUin),
			Comment:       "",
			Flag:          flag,
			Extra: map[string]string{
				"invite_uin":  fmt.Sprintf("%v", uin),
				"invite_nick": event.InvitorNick,
				"invitor_uid": event.InvitorUID,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupMessageRecalled(cli *client.QQClient, event *event.GroupRecall) int32 {
	opuin := cli.GetUin(event.OperatorUID, event.GroupUin)
	auuin := cli.GetUin(event.UserUID, event.GroupUin)
	if event.UserUID == event.OperatorUID {
		log.Infof("群 %v 内 %v(%s) 撤回了一条消息, 消息Id为 %v", event.GroupUin, auuin, event.UserUID, event.Sequence)
	} else {
		log.Infof("群 %v 内 %v(%s) 撤回了 %v(%s) 的一条消息, 消息Id为 %v", event.GroupUin, opuin, event.OperatorUID, auuin, event.UserUID, event.Sequence)
	}
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupRecallNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_GroupRecallNoticeEvent{
		GroupRecallNoticeEvent: &onebot.GroupRecallNoticeEvent{
			Time:           time.Now().Unix(),
			SelfId:         int64(cli.Uin),
			PostType:       "notice",
			NoticeType:     "group_recall",
			GroupId:        int64(event.GroupUin),
			Extra: map[string]string{
				"author_uid":      event.UserUID,
				"operator_uid": event.OperatorUID,
				"sequence":       fmt.Sprintf("%v",event.Sequence),
				"random":         fmt.Sprintf("%v",event.Random),
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportFriendMessageRecalled(cli *client.QQClient, event *event.FriendRecall) int32 {
	log.Infof("好友 %s 撤回了一条消息, 消息Id为 %v", event.FromUID, event.Sequence)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendRecallNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_FriendRecallNoticeEvent{
		FriendRecallNoticeEvent: &onebot.FriendRecallNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "friend_recall",
			MessageId:  int32(event.Sequence),
			Extra: map[string]string{
				"from_uid":    event.FromUID,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportNewFriendAdded(cli *client.QQClient, event *event.NewFriendRequest) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendAddNoticeEvent,
	}
	eventProto.Data = &onebot.Frame_FriendAddNoticeEvent{
		FriendAddNoticeEvent: &onebot.FriendAddNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "friend_add",
			UserId:     int64(event.SourceUin),
			Extra: map[string]string{
				"source_uin": fmt.Sprintf("%v", event.SourceUin),
				"source_uid": event.SourceUID,
				"source":     event.Source,
				"msg":        event.Msg,
			},
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}
