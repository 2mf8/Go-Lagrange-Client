package bot

import (
	"fmt"
	"html"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/message"
)

func MiraiMsgToRawMsg(cli *client.QQClient, messageChain []message.IMessageElement) string {
	result := ""
	for _, element := range messageChain {
		switch elem := element.(type) {
		case *message.TextElement:
			result += elem.Content
		case *message.ImageElement:
			result += fmt.Sprintf(`<image image_id="%s" url="%s"/>`, html.EscapeString(elem.ImageID), html.EscapeString(elem.URL))
		case *message.FaceElement:
			result += fmt.Sprintf(`<face id="%d"/>`, elem.FaceID)
		case *message.VoiceElement:
			result += fmt.Sprintf(`<voice url="%s"/>`, html.EscapeString(elem.URL))
		case *message.ReplyElement:
			result += fmt.Sprintf(`<reply time="%d" sender="%d" raw_message="%s" reply_seq="%d"/>`, elem.Time, elem.SenderUin, html.EscapeString(MiraiMsgToRawMsg(cli, elem.Elements)), elem.ReplySeq)
		case *message.ForwardMessage:
			result += MiraiForwardToRawForward(cli, elem)
		}
	}
	return result
}

func MiraiForwardToRawForward(cli *client.QQClient, elem *message.ForwardMessage) string {
	result := fmt.Sprintf(`<forward res_id = "%s"/>`, elem.ResID)
	return result
}
