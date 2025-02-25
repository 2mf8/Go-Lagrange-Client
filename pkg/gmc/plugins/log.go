package plugins

import (
	"github.com/2mf8/Go-Lagrange-Client/pkg/bot"
	"github.com/2mf8/Go-Lagrange-Client/pkg/plugin"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/message"
	log "github.com/sirupsen/logrus"
)

func LogPrivateMessage(cli *client.QQClient, event *message.PrivateMessage) int32 {
	cli.MarkPrivateMessageReaded(event.Sender.Uin, event.Time, event.ID)
	log.Infof("Bot(%+v) Private(%+v) -> %+v\n", cli.Uin, event.Sender.Uin, bot.MiraiMsgToRawMsg(cli, event.Elements))
	return plugin.MessageIgnore
}

func LogGroupMessage(cli *client.QQClient, event *message.GroupMessage) int32 {
	cli.MarkGroupMessageReaded(event.GroupUin, event.ID)
	log.Infof("Bot(%+v) Group(%+v) Sender(%+v) -> %+v\n", cli.Uin, event.GroupUin, event.Sender.Uin, bot.MiraiMsgToRawMsg(cli, event.Elements))
	return plugin.MessageIgnore
}
