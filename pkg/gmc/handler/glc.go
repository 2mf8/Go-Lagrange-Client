package handler

import (
	_ "fmt"
	"reflect"
	_ "unsafe"

	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/client/auth"
	"github.com/LagrangeDev/LagrangeGo/client/packets/tlv"
	"github.com/LagrangeDev/LagrangeGo/utils"
	"github.com/LagrangeDev/LagrangeGo/utils/binary"
)

//go:linkname version github.com/LagrangeDev/LagrangeGo/client.(*QQClient).version
func version(c *client.QQClient) *auth.AppInfo

//go:linkname sendUniPacketAndWait github.com/LagrangeDev/LagrangeGo/client.(*QQClient).sendUniPacketAndWait
func sendUniPacketAndWait(c *client.QQClient, cmd string, buf []byte) ([]byte, error)

//go:linkname buildLoginPacket github.com/LagrangeDev/LagrangeGo/client.(*QQClient).buildLoginPacket
func buildLoginPacket(c *client.QQClient, uin uint32, cmd string, body []byte) []byte

//go:linkname decodeLoginResponse github.com/LagrangeDev/LagrangeGo/client.(*QQClient).decodeLoginResponse
func decodeLoginResponse(c *client.QQClient, buf []byte, sig *auth.SigInfo) error

func GetT106AndT16a(c *client.QQClient) (t106 []byte, t16a []byte) {
	v := reflect.ValueOf(c)
	return v.Elem().FieldByName("t106").Bytes(), v.Elem().FieldByName("t16a").Bytes()
}

func QRCodeConfirmedAfter(c *client.QQClient) error {
	app := version(c)
	device := c.Device()
	t106, t16a := GetT106AndT16a(c)
	response, err := sendUniPacketAndWait(
		c,
		"wtlogin.login",
		buildLoginPacket(c, c.Uin, "wtlogin.login", binary.NewBuilder(nil).
			WriteU16(0x09).
			WriteTLV(
				binary.NewBuilder(nil).WriteBytes(t106).Pack(0x106),
				tlv.T144(c.Sig().Tgtgt, app, device),
				tlv.T116(app.SubSigmap),
				tlv.T142(app.PackageName, 0),
				tlv.T145(utils.MustParseHexStr(device.Guid)),
				tlv.T18(0, app.AppClientVersion, int(c.Uin), 0, 5, 0),
				tlv.T141([]byte("Unknown"), nil),
				tlv.T177(app.WTLoginSDK, 0),
				tlv.T191(0),
				tlv.T100(5, app.AppID, app.SubAppID, 8001, app.MainSigmap, 0),
				tlv.T107(1, 0x0d, 0, 1),
				tlv.T318(nil),
				binary.NewBuilder(nil).WriteBytes(t16a).Pack(0x16a),
				tlv.T166(5),
				tlv.T521(0x13, "basicim"),
			).ToBytes()))

	if err != nil {
		return err
	}

	return decodeLoginResponse(c, response, c.Sig())
}
