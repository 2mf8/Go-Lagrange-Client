package plugins

import (
	"strconv"
	"time"

	"github.com/2mf8/Go-Lagrange-Client/pkg/bot"
	"github.com/2mf8/Go-Lagrange-Client/pkg/cache"
	"github.com/2mf8/Go-Lagrange-Client/pkg/plugin"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/onebot"
	log "github.com/sirupsen/logrus"

	"github.com/2mf8/LagrangeGo/client"
	"github.com/2mf8/LagrangeGo/client/event"
	"github.com/2mf8/LagrangeGo/message"
)

func ReportPrivateMessage(cli *client.QQClient, event *message.PrivateMessage) int32 {
	cache.PrivateMessageLru.Add(event.Id, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TPrivateMessageEvent,
	}
	eventProto.PbData = &onebot.Frame_PrivateMessageEvent{
		PrivateMessageEvent: &onebot.PrivateMessageEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "message",
			MessageType: "private",
			SubType:     "normal",
			MessageId:   event.Id,
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
	cache.GroupMessageLru.Add(event.Id, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupMessageEvent,
	}
	groupMessageEvent := &onebot.GroupMessageEvent{
		Time:        time.Now().Unix(),
		SelfId:      int64(cli.Uin),
		PostType:    "message",
		MessageType: "group",
		SubType:     "normal",
		MessageId:   event.Id,
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

	eventProto.PbData = &onebot.Frame_GroupMessageEvent{
		GroupMessageEvent: groupMessageEvent,
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportMemberJoin(cli *client.QQClient, event *event.GroupMemberIncrease) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupIncreaseNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupIncreaseNoticeEvent{
		GroupIncreaseNoticeEvent: &onebot.GroupIncreaseNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "message",
			NoticeType: "group_increase",
			SubType:    "approve",
			GroupId:    int64(event.GroupUin),
			UserId:     0,
			OperatorId: 0,
			MemberUid:  event.MemberUid,
			InvitorUid: event.InvitorUid,
			JoinType:   event.JoinType,
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
		operatorUid = event.OperatorUid
	}

	eventProto.PbData = &onebot.Frame_GroupDecreaseNoticeEvent{
		GroupDecreaseNoticeEvent: &onebot.GroupDecreaseNoticeEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "message",
			NoticeType:  "group_decrease",
			SubType:     subType,
			GroupId:     int64(event.GroupUin),
			MemberUid:   event.MemberUid,
			OperatorUid: operatorUid,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportJoinGroup(cli *client.QQClient, event *event.GroupMemberIncrease) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupIncreaseNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupIncreaseNoticeEvent{
		GroupIncreaseNoticeEvent: &onebot.GroupIncreaseNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "message",
			NoticeType: "group_increase",
			SubType:    "approve",
			GroupId:    int64(event.GroupUin),
			UserId:     int64(cli.Uin),
			OperatorId: 0,
			MemberUid:  event.MemberUid,
			JoinType:   event.JoinType,
			InvitorUid: event.InvitorUid,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupMute(cli *client.QQClient, event *event.GroupMute) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupBanNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupBanNoticeEvent{
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
			GroupId:     int64(event.GroupUin),
			OperatorUid: event.OperatorUid,
			TargetUid:   event.TargetUid,
			Duration:    int64(event.Duration),
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportNewFriendRequest(cli *client.QQClient, event *event.NewFriendRequest) int32 {
	flag := strconv.FormatInt(int64(event.SourceUin), 10)
	cache.FriendRequestLru.Add(flag, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendRequestEvent,
	}
	eventProto.PbData = &onebot.Frame_FriendRequestEvent{
		FriendRequestEvent: &onebot.FriendRequestEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "request",
			RequestType: "friend",
			Flag:        flag,
			SourceUid:   event.SourceUid,
			Msg:         event.Msg,
			Source:      event.Source,
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
	eventProto.PbData = &onebot.Frame_GroupRequestEvent{
		GroupRequestEvent: &onebot.GroupRequestEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "request",
			RequestType: "group",
			SubType:     "add",
			GroupId:     int64(event.GroupUin),
			Flag:        flag,
			TargetUid:   event.TargetUid,
			InvitorUid:  event.InvitorUid,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupInvitedRequest(cli *client.QQClient, event *event.GroupInvite) int32 {
	flag := strconv.FormatInt(int64(event.GroupUin), 10)
	cache.GroupInvitedRequestLru.Add(flag, event)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupRequestEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupRequestEvent{
		GroupRequestEvent: &onebot.GroupRequestEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "request",
			RequestType: "group",
			SubType:     "invite",
			GroupId:     int64(event.GroupUin),
			InvitorUid:  event.InvitorUid,
			Comment:     "",
			Flag:        flag,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportGroupMessageRecalled(cli *client.QQClient, event *event.GroupRecall) int32 {
	opuin := cli.GetUin(event.OperatorUid, event.GroupUin)
	auuin := cli.GetUin(event.AuthorUid, event.GroupUin)
	if event.AuthorUid == event.OperatorUid {
		log.Infof("群 %v 内 %v(%s) 撤回了一条消息, 消息Id为 %v", event.GroupUin, auuin, event.AuthorUid, event.Sequence)
	} else {
		log.Infof("群 %v 内 %v(%s) 撤回了 %v(%s) 的一条消息, 消息Id为 %v", event.GroupUin, opuin, event.OperatorUid, auuin, event.AuthorUid, event.Sequence)
	}
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupRecallNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupRecallNoticeEvent{
		GroupRecallNoticeEvent: &onebot.GroupRecallNoticeEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "notice",
			NoticeType:  "group_recall",
			GroupId:     int64(event.GroupUin),
			AuthorUid:   event.AuthorUid,
			OperatorUid: event.OperatorUid,
			Sequence:    event.Sequence,
			Random:      event.Random,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportFriendMessageRecalled(cli *client.QQClient, event *event.FriendRecall) int32 {
	log.Infof("好友 %s 撤回了一条消息, 消息Id为 %v", event.FromUid, event.Sequence)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendRecallNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_FriendRecallNoticeEvent{
		FriendRecallNoticeEvent: &onebot.FriendRecallNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "friend_recall",
			FromUid:    event.FromUid,
			MessageId:  int32(event.Sequence),
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportNewFriendAdded(cli *client.QQClient, event *event.NewFriendRequest) int32 {
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TFriendAddNoticeEvent,
	}
	eventProto.PbData = &onebot.Frame_FriendAddNoticeEvent{
		FriendAddNoticeEvent: &onebot.FriendAddNoticeEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "friend_add",
			UserId:     int64(event.SourceUin),
			SourceUin:  event.SourceUin,
			SourceUid:  event.SourceUid,
			Source:     event.Source,
			Msg:        event.Msg,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportPoke(cli *client.QQClient, ievent event.INotifyEvent) int32 {
	gp, ok := ievent.(*event.GroupPokeEvent)
	if ok {
		if gp.Suffix != "" {
			log.Infof("群 %v 内 %d%s%d的%s", gp.GroupUin, gp.Sender, gp.Action, gp.Receiver, gp.Suffix)
		} else {
			log.Infof("群 %v 内 %d%s%d", gp.GroupUin, gp.Sender, gp.Action, gp.Receiver)
		}
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TGroupPokeEvent,
		}
		eventProto.PbData = &onebot.Frame_GroupPokeEvent{
			GroupPokeEvent: &onebot.GroupPokeEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "group_poke",
				GroupUin:   gp.GroupUin,
				Sender:     gp.Sender,
				Receiver:   gp.Receiver,
				Suffix:     gp.Suffix,
				Action:     gp.Action,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	}
	pp, ok := ievent.(*event.FriendPokeEvent)
	if ok {
		if pp.Suffix != "" {
			log.Infof("%d%s%d的%s", pp.Sender, pp.Action, pp.Receiver, pp.Suffix)
		} else {
			log.Infof("%d%s%d", pp.Sender, pp.Action, pp.Receiver)
		}
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TFriendPokeEvent,
		}
		eventProto.PbData = &onebot.Frame_FriendPokeEvent{
			FriendPokeEvent: &onebot.FriendPokeEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "friend_poke",
				Sender:     pp.Sender,
				Receiver:   pp.Receiver,
				Suffix:     pp.Suffix,
				Action:     pp.Action,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	}
	return plugin.MessageIgnore
}

func ReportGroupDigest(cli *client.QQClient, event *event.GroupDigestEvent) int32 {
	if event.OperationType == 1 {
		log.Infof("群 %v 内 %s 的消息被 %s 设置了精华消息", event.GroupUin, event.SenderNick, event.OperatorNick)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TGroupDigestEvent,
		}
		eventProto.PbData = &onebot.Frame_GroupDigestEvent{
			GroupDigestEvent: &onebot.GroupDigestEvent{
				Time:              time.Now().Unix(),
				SelfId:            int64(cli.Uin),
				PostType:          "notice",
				NoticeType:        "group_digest",
				SubType:           "set",
				GroupUin:          event.GroupUin,
				MessageId:         event.MessageID,
				InternalMessageId: event.InternalMessageID,
				OperationType:     event.OperationType,
				OperationTime:     event.OperateTime,
				SenderUin:         event.SenderUin,
				OperatorUin:       event.OperatorUin,
				SenderNick:        event.SenderNick,
				OperationNick:     event.OperatorNick,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	} else {
		log.Infof("群 %v 内 %s 的消息被 %s 取消了精华消息", event.GroupUin, event.SenderNick, event.OperatorNick)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TGroupDigestEvent,
		}
		eventProto.PbData = &onebot.Frame_GroupDigestEvent{
			GroupDigestEvent: &onebot.GroupDigestEvent{
				Time:              time.Now().Unix(),
				SelfId:            int64(cli.Uin),
				PostType:          "notice",
				NoticeType:        "group_digest",
				SubType:           "cancel",
				GroupUin:          event.GroupUin,
				MessageId:         event.MessageID,
				InternalMessageId: event.InternalMessageID,
				OperationType:     event.OperationType,
				OperationTime:     event.OperateTime,
				SenderUin:         event.SenderUin,
				OperatorUin:       event.OperatorUin,
				SenderNick:        event.SenderNick,
				OperationNick:     event.OperatorNick,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	}
	return plugin.MessageIgnore
}

func ReportGroupMemberPermissionChanged(cli *client.QQClient, event *event.GroupMemberPermissionChanged) int32 {
	if event.IsAdmin {
		log.Infof("群 %v 内 %v(%s) 成为了管理员", event.GroupUin, event.TargetUin, event.TargetUid)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TGroupMemberPermissionChangedEvent,
		}
		eventProto.PbData = &onebot.Frame_GroupMemberPermissionChangeEvent{
			GroupMemberPermissionChangeEvent: &onebot.GroupMemberPermissionChangedEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "group_admin",
				SubType:    "set",
				GroupEvent: &onebot.GroupMessageEvent{
					GroupId: int64(event.GroupUin),
				},
				TargetUin: event.TargetUin,
				TargetUid: event.TargetUid,
				IsAdmin:   event.IsAdmin,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	} else {
		log.Infof("群 %v 内 %v(%s) 取消了管理员", event.GroupUin, event.TargetUin, event.TargetUid)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TGroupMemberPermissionChangedEvent,
		}
		eventProto.PbData = &onebot.Frame_GroupMemberPermissionChangeEvent{
			GroupMemberPermissionChangeEvent: &onebot.GroupMemberPermissionChangedEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "group_admin",
				SubType:    "cancel",
				GroupEvent: &onebot.GroupMessageEvent{
					GroupId: int64(event.GroupUin),
				},
				TargetUin: event.TargetUin,
				TargetUid: event.TargetUid,
				IsAdmin:   event.IsAdmin,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	}
	return plugin.MessageIgnore
}

func ReportGroupNameUpdated(cli *client.QQClient, event *event.GroupNameUpdated) int32 {
	log.Infof("群 %v 的名字变为了 %s ,操作者为 %v(%s)", event.GroupUin, event.NewName, event.OperatorUin, event.OperatorUid)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TGroupNameUpdatedEvent,
	}
	eventProto.PbData = &onebot.Frame_GroupNameUpdatedEvent{
		GroupNameUpdatedEvent: &onebot.GroupNameUpdatedEvent{
			Time:        time.Now().Unix(),
			SelfId:      int64(cli.Uin),
			PostType:    "notice",
			NoticeType:  "group_name",
			SubType:     "change",
			GroupUin:    event.GroupUin,
			NewName:     event.NewName,
			OperatorUin: event.OperatorUin,
			OperatorUid: event.OperatorUid,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportMemberSpecialTitleUpdated(cli *client.QQClient, event *event.MemberSpecialTitleUpdated) int32 {
	log.Infof("群 %v 内 %v 获得了 %s 头衔", event.GroupUin, event.Uin, event.NewTitle)
	eventProto := &onebot.Frame{
		FrameType: onebot.Frame_TMemberSpecialTitleUpdatedEvent,
	}
	eventProto.PbData = &onebot.Frame_MemberSpecialTitleUpdatedEvent{
		MemberSpecialTitleUpdatedEvent: &onebot.MemberSpecialTitleUpdatedEvent{
			Time:       time.Now().Unix(),
			SelfId:     int64(cli.Uin),
			PostType:   "notice",
			NoticeType: "group_member_titile",
			SubType:    "change",
			GroupUin:   event.GroupUin,
			Uin:        event.Uin,
			NewTitle:   event.NewTitle,
		},
	}
	bot.HandleEventFrame(cli, eventProto)
	return plugin.MessageIgnore
}

func ReportRename(cli *client.QQClient, event *event.Rename) int32 {
	if event.SubType == 0 {
		log.Infof("机器人修改了自己的昵称为 %s", event.Nickname)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TRenameEvent,
		}
		eventProto.PbData = &onebot.Frame_RenameEvent{
			RenameEvent: &onebot.RenameEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "rename",
				SubType:    "self",
				Uin:        event.Uin,
				Uid:        event.Uid,
				NickName:   event.Nickname,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	} else {
		log.Infof("%v(%s) 的昵称被修改为 %s", event.Uin, event.Uid, event.Nickname)
		eventProto := &onebot.Frame{
			FrameType: onebot.Frame_TRenameEvent,
		}
		eventProto.PbData = &onebot.Frame_RenameEvent{
			RenameEvent: &onebot.RenameEvent{
				Time:       time.Now().Unix(),
				SelfId:     int64(cli.Uin),
				PostType:   "notice",
				NoticeType: "rename",
				SubType:    "other",
				Uin:        event.Uin,
				Uid:        event.Uid,
				NickName:   event.Nickname,
			},
		}
		bot.HandleEventFrame(cli, eventProto)
	}
	return plugin.MessageIgnore
}
