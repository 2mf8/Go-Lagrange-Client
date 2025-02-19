package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/2mf8/Go-Lagrange-Client/pkg/bot"
	"github.com/2mf8/Go-Lagrange-Client/pkg/config"
	"github.com/2mf8/Go-Lagrange-Client/pkg/device"
	"github.com/2mf8/Go-Lagrange-Client/pkg/gmc/plugins"
	"github.com/2mf8/Go-Lagrange-Client/pkg/plugin"
	"github.com/2mf8/Go-Lagrange-Client/pkg/util"
	"github.com/2mf8/Go-Lagrange-Client/proto_gen/dto"
	"github.com/fanliao/go-promise"

	"github.com/BurntSushi/toml"
	_ "github.com/BurntSushi/toml"
	"github.com/LagrangeDev/LagrangeGo/client"
	"github.com/LagrangeDev/LagrangeGo/client/auth"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
)

type WaitingCaptcha struct {
	Captcha *dto.Bot_Captcha
	Prom    *promise.Promise
}

var queryQRCodeMutex = &sync.RWMutex{}
var qrCodeBot *client.QQClient

var WaitingCaptchas CaptchaMap

var AppList = device.GetAppList()

type QRCodeResp int

const (
	Unknown = iota
	QRCodeImageFetch
	QRCodeWaitingForScan
	QRCodeWaitingForConfirm
	QRCodeTimeout
	QRCodeConfirmed
	QRCodeCanceled
)

func TokenLogin() {
	set := config.ReadSetting()
	dfs, err := os.ReadDir("./device/")
	if err == nil {
		for _, v := range dfs {
			df := strings.Split(v.Name(), ".")
			uin, err := strconv.ParseInt(df[0], 10, 64)
			if err == nil {
				devi := device.GetDevice(uin)
				sfs, err := os.ReadDir("./sig/")
				if err == nil {
					for _, sv := range sfs {
						sf := strings.Split(sv.Name(), ".")
						if df[0] == sf[0] {
							sigpath := fmt.Sprintf("./sig/%s", sv.Name())
							data, err := os.ReadFile(sigpath)
							if err == nil {
								sig, err := auth.UnmarshalSigInfo(data, true)
								if err == nil {
									go func() {
										queryQRCodeMutex.Lock()
										defer queryQRCodeMutex.Unlock()
										appInfo := auth.AppList[set.Platform][set.AppVersion]
										cli := client.NewClient(0, "")
										cli.UseVersion(appInfo)
										cli.UseDevice(devi)
										cli.AddSignServer(set.SignServer)
										cli.UseSig(sig)
										cli.FastLogin()
										bot.Clients.Store(int64(cli.Uin), cli)
										go AfterLogin(cli)
									}()
								}
							}
						}
					}
				}
			} else {
				fmt.Printf("转换账号%s失败", df[0])
			}
		}
	}
}

func TokenReLogin(userId int64, retryInterval int, retryCount int) {
	set := config.ReadSetting()
	cli, ok := bot.Clients.Load(userId)
	if !ok {
		log.Warnf("%v 不存在，登录失败", userId)
	} else {
		var times = 0
		log.Warnf("Bot已离线 (%v)，将在 %v 秒后尝试重连. 重连次数：%v",
			cli.Uin, retryInterval, times)
		if cli.Online.Load() {
			log.Warn("Bot已登录")
			return
		}
		if times < retryCount {
			times++
			cli.Disconnect()
			bot.Clients.Delete(int64(cli.Uin))
			bot.ReleaseClient(cli)
			time.Sleep(time.Second * time.Duration(retryInterval))
			devi := device.GetDevice(userId)
			sigpath := fmt.Sprintf("./sig/%v.bin", userId)
			fmt.Println(sigpath)
			data, err := os.ReadFile(sigpath)
			if err == nil {
				sig, err := auth.UnmarshalSigInfo(data, true)
				if err == nil {
					log.Warnf("%v 第 %v 次登录尝试", userId, times)
					appInfo := auth.AppList[set.Platform][set.AppVersion]
					cli := client.NewClient(0, "")
					cli.UseVersion(appInfo)
					cli.UseDevice(devi)
					cli.AddSignServer(set.SignServer)
					cli.UseSig(sig)
					cli.FastLogin()
					bot.Clients.Store(userId, cli)
					go AfterLogin(cli)
				} else {
					log.Warnf("%v 第 %v 次登录失败, 120秒后重试", userId, times)
				}
			} else {
				log.Warnf("%v 第 %v 次登录失败, 120秒后重试", userId, times)
			}
		} else {
			log.Errorf("failed to reconnect: 重连次数达到设置的上限值, %+v", cli.Uin)
		}
	}
}

func init() {
	log.Infof("加载日志插件 Log")
	plugin.AddPrivateMessagePlugin(plugins.LogPrivateMessage)
	plugin.AddGroupMessagePlugin(plugins.LogGroupMessage)
	log.Infof("加载测试插件 Hello")
	plugin.AddPrivateMessagePlugin(plugins.HelloPrivateMessage)
	plugin.AddGroupMessagePlugin(plugins.HelloGroupMessage)
	log.Infof("加载上报插件 Report")
	plugin.AddGroupInvitedRequestPlugin(plugins.ReportGroupInvitedRequest)
	plugin.AddPrivateMessagePlugin(plugins.ReportPrivateMessage)
	plugin.AddGroupMessagePlugin(plugins.ReportGroupMessage)
	plugin.AddMemberJoinGroupPlugin(plugins.ReportMemberJoin)
	plugin.AddMemberLeaveGroupPlugin(plugins.ReportMemberLeave)
	plugin.AddNewFriendRequestPlugin(plugins.ReportNewFriendRequest)
	plugin.AddGroupMessageRecalledPlugin(plugins.ReportGroupMessageRecalled)
	plugin.AddFriendMessageRecalledPlugin(plugins.ReportFriendMessageRecalled)
	plugin.AddNewFriendAddedPlugin(plugins.ReportNewFriendAdded)
	plugin.AddGroupMutePlugin(plugins.ReportGroupMute)
	plugin.AddNotifyPlugin(plugins.ReportPoke)
	plugin.AddGroupDigestPlugin(plugins.ReportGroupDigest)
	plugin.AddGroupMemberPermissionChangedPlugin(plugins.ReportGroupMemberPermissionChanged)
	plugin.AddGroupNameUpdated(plugins.ReportGroupNameUpdated)
	plugin.AddMemberSpecialTitleUpdated(plugins.ReportMemberSpecialTitleUpdated)
	plugin.AddRenameEvent(plugins.ReportRename)
}

func DeleteBot(c *gin.Context) {
	req := &dto.DeleteBotReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	cli, ok := bot.Clients.Load(req.BotId)
	if !ok {
		c.String(http.StatusBadRequest, "bot not exists")
		return
	}
	go func() {
		queryQRCodeMutex.Lock()
		defer queryQRCodeMutex.Unlock()
		sigpath := fmt.Sprintf("./sig/%v.bin", cli.Uin)
		sigDir := path.Dir(sigpath)
		if !util.PathExists(sigDir) {
			log.Infof("%+v 目录不存在，自动创建", sigDir)
			if err := os.MkdirAll(sigDir, 0777); err != nil {
				log.Warnf("failed to mkdir deviceDir, err: %+v", err)
			}
		}
		data, err := cli.Sig().Marshal()
		if err != nil {
			log.Errorln("marshal sig.bin err:", err)
			return
		}
		err = os.WriteFile(sigpath, data, 0644)
		if err != nil {
			log.Errorln("write sig.bin err:", err)
			return
		}
		log.Infoln("sig saved into sig.bin")
	}()
	bot.Clients.Delete(int64(cli.Uin))
	bot.ReleaseClient(cli)
	resp := &dto.DeleteBotResp{}
	Return(c, resp)
}

func GetAllVersion(c *gin.Context) {
	req := &dto.GetAllVersionReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	var resp = &dto.GetAllVersionResp{
		AllVersion: []*dto.GetAllVersionResp_AllVersion{},
	}
	resp = device.GetAppList()
	Return(c, resp)
}

func SetBaseInfo(c *gin.Context) {
	req := &dto.SetBaseInfoReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	if req.Platform == "default" {
		config.AllSetting.Platform = "linux"
	} else {
		config.AllSetting.Platform = req.Platform
	}
	if req.AppVersion == "default" {
		config.AllSetting.AppVersion = "3.2.10-25765"
	} else {
		config.AllSetting.AppVersion = req.AppVersion
	}
	if req.SignServer == "default" {
		config.AllSetting.SignServer = "https://sign.lagrangecore.org/api/sign/25765"
	} else {
		config.AllSetting.SignServer = req.SignServer
	}
	filepath := "./setting/setting.toml"
	file, _ := os.Create(filepath)
	defer file.Close()
	toml.NewEncoder(file).Encode(config.AllSetting)
	var resp = &dto.SetBaseInfoResp{}
	Return(c, resp)
}

func ListBot(c *gin.Context) {
	req := &dto.ListBotReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	var resp = &dto.ListBotResp{
		BotList: []*dto.Bot{},
	}
	bot.Clients.Range(func(_ int64, cli *client.QQClient) bool {
		resp.BotList = append(resp.BotList, &dto.Bot{
			BotId:    int64(cli.Uin),
			IsOnline: cli.Online.Load(),
		})
		return true
	})
	Return(c, resp)
}

func CreateBot(c *gin.Context) {
	req := &dto.CreateBotReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	if req.BotId == 0 {
		c.String(http.StatusBadRequest, "botId is 0")
		return
	}
	_, ok := bot.Clients.Load(req.BotId)
	if ok {
		c.String(http.StatusInternalServerError, "botId already exists")
		return
	}
	go func() {
		PasswordLogin(uint32(req.BotId), req.Password)
	}()
	resp := &dto.CreateBotResp{}
	Return(c, resp)
}

func FetchQrCode(c *gin.Context) {
	set := config.ReadSetting()
	req := &dto.FetchQRCodeReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request, not protobuf")
		return
	}
	newDeviceInfo := device.GetDevice(req.DeviceSeed)
	appInfo := auth.AppList[set.Platform][set.AppVersion]
	qqclient := client.NewClient(0, "")
	qqclient.UseVersion(appInfo)
	qqclient.UseDevice(newDeviceInfo)
	qqclient.AddSignServer(set.SignServer)
	qrCodeBot = qqclient
	b, s, err := qrCodeBot.FetchQRCode(3, 4, 2)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Sprintf("failed to fetch qrcode, %+v", err))
		return
	}
	resp := &dto.QRCodeLoginResp{
		State:     dto.QRCodeLoginResp_QRCodeLoginState(http.StatusOK),
		ImageData: b,
		Sig:       []byte(s),
	}
	Return(c, resp)
}

func QueryQRCodeStatus(c *gin.Context) {
	respCode := 0
	ok, err := qrCodeBot.GetQRCodeResult()
	//fmt.Println(ok.Name(), ok.Waitable(), ok.Success(), err)
	if err != nil {
		resp := &dto.QRCodeLoginResp{
			State: dto.QRCodeLoginResp_QRCodeLoginState(http.StatusExpectationFailed),
		}
		Return(c, resp)
	}
	fmt.Println(ok.Name())
	if !ok.Success() {
		resp := &dto.QRCodeLoginResp{
			State: dto.QRCodeLoginResp_QRCodeLoginState(http.StatusExpectationFailed),
		}
		Return(c, resp)
	}
	if ok.Name() == "WaitingForConfirm" {
		respCode = QRCodeWaitingForScan
	}
	if ok.Name() == "Canceled" {
		respCode = QRCodeCanceled
	}
	if ok.Name() == "WaitingForConfirm" {
		respCode = QRCodeWaitingForConfirm
	}
	if ok.Name() == "Confirmed" {
		respCode = QRCodeConfirmed
		go func() {
			queryQRCodeMutex.Lock()
			defer queryQRCodeMutex.Unlock()
			_, err := qrCodeBot.QRCodeLogin()
			if err != nil {
				fmt.Println(err)
			}
			time.Sleep(time.Second * 5)
			log.Infof("登录成功")
			originCli, ok := bot.Clients.Load(int64(qrCodeBot.Uin))

			// 重复登录，旧的断开
			if ok {
				originCli.Release()
			}
			bot.Clients.Store(int64(qrCodeBot.Uin), qrCodeBot)
			go AfterLogin(qrCodeBot)
			qrCodeBot = nil
		}()
	}
	if ok.Name() == "Expired" {
		respCode = QRCodeTimeout
	}
	resp := &dto.QRCodeLoginResp{
		State: dto.QRCodeLoginResp_QRCodeLoginState(respCode),
	}
	Return(c, resp)
}

func ListPlugin(c *gin.Context) {
	req := &dto.ListPluginReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	var resp = &dto.ListPluginResp{
		Plugins: []*dto.Plugin{},
	}
	config.Plugins.Range(func(key string, p *config.Plugin) bool {
		urls := []string{}
		url := strings.Join(p.Urls, ",")
		urls = append(urls, url)
		resp.Plugins = append(resp.Plugins, &dto.Plugin{
			Name:         p.Name,
			Disabled:     p.Disabled,
			Json:         p.Json,
			Protocol:     p.Protocol,
			Urls:         urls,
			EventFilter:  p.EventFilter,
			ApiFilter:    p.ApiFilter,
			RegexFilter:  p.RegexFilter,
			RegexReplace: p.RegexReplace,
			ExtraHeader: func() []*dto.Plugin_Header {
				headers := make([]*dto.Plugin_Header, 0)
				for k, v := range p.ExtraHeader {
					headers = append(headers, &dto.Plugin_Header{
						Key:   k,
						Value: v,
					})
				}
				return headers
			}(),
		})
		return true
	})
	Return(c, resp)
}

func SavePlugin(c *gin.Context) {
	req := &dto.SavePluginReq{}
	urls := []string{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	if req.Plugin == nil {
		c.String(http.StatusBadRequest, "plugin is nil")
		return
	}
	p := req.Plugin
	if p.ApiFilter == nil {
		p.ApiFilter = []int32{}
	}
	if p.EventFilter == nil {
		p.EventFilter = []int32{}
	}
	if p.Urls != nil {
		_urls := strings.Split(req.Plugin.Urls[0], ",")
		for _, v := range _urls {
			if v != "" {
				urls = append(urls, strings.TrimSpace(v))
			}
		}
	}
	config.Plugins.Store(p.Name, &config.Plugin{
		Name:         p.Name,
		Disabled:     p.Disabled,
		Json:         p.Json,
		Protocol:     p.Protocol,
		Urls:         urls,
		EventFilter:  p.EventFilter,
		ApiFilter:    p.ApiFilter,
		RegexFilter:  p.RegexFilter,
		RegexReplace: p.RegexReplace,
		ExtraHeader: func() map[string][]string {
			headers := map[string][]string{}
			for _, h := range p.ExtraHeader {
				headers[h.Key] = h.Value
			}
			return headers
		}(),
	})
	config.WritePlugins()
	resp := &dto.SavePluginResp{}
	Return(c, resp)
}

func DeletePlugin(c *gin.Context) {
	req := &dto.DeletePluginReq{}
	err := Bind(c, req)
	if err != nil {
		c.String(http.StatusBadRequest, "bad request")
		return
	}
	config.Plugins.Delete(req.Name)
	config.WritePlugins()
	resp := &dto.DeletePluginResp{}
	Return(c, resp)
}

func Return(c *gin.Context, resp proto.Message) {
	var (
		data []byte
		err  error
	)
	switch c.ContentType() {
	case binding.MIMEPROTOBUF:
		data, err = proto.Marshal(resp)
	case binding.MIMEJSON:
		data, err = json.Marshal(resp)
	}
	if err != nil {
		c.String(http.StatusInternalServerError, "marshal resp error")
		return
	}
	c.Data(http.StatusOK, c.ContentType(), data)
}

func AfterLogin(cli *client.QQClient) {
	for {
		time.Sleep(5 * time.Second)
		if cli.Online.Load() {
			break
		}
		log.Warnf("机器人不在线，可能在等待输入验证码，或出错了。如果出错请重启。")
	}
	plugin.Serve(cli)
	log.Infof("插件加载完成")

	log.Infof("刷新好友列表")
	if fs, err := cli.GetFriendsData(); err != nil {
		util.FatalError(fmt.Errorf("failed to load friend list, err: %+v", err))
	} else {
		log.Infof("共加载 %v 个好友.", len(fs))
	}
	go InitForwardGin(cli)
	bot.ConnectUniversal(cli)

	defer cli.Release()
	defer func() {
		sigpath := fmt.Sprintf("./sig/%v.bin", cli.Uin)
		sigDir := path.Dir(sigpath)
		if !util.PathExists(sigDir) {
			log.Infof("%+v 目录不存在，自动创建", sigDir)
			if err := os.MkdirAll(sigDir, 0777); err != nil {
				log.Warnf("failed to mkdir deviceDir, err: %+v", err)
			}
		}
		data, err := cli.Sig().Marshal()
		if err != nil {
			log.Errorln("marshal sig.bin err:", err)
			return
		}
		err = os.WriteFile(sigpath, data, 0644)
		if err != nil {
			log.Errorln("write sig.bin err:", err)
			return
		}
		log.Infoln("sig saved into sig.bin")
	}()

	// setup the main stop channel
	mc := make(chan os.Signal, 2)
	signal.Notify(mc, os.Interrupt, syscall.SIGTERM)
	for {
		switch <-mc {
		case os.Interrupt, syscall.SIGTERM:
			return
		}
	}
}

func InitForwardGin(cli *client.QQClient) {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	if len(config.HttpAuth) > 0 {
		router.Use(gin.BasicAuth(config.HttpAuth))
	}

	router.Use(CORSMiddleware())
	router.GET("/onebot/ws", func(c *gin.Context) {
		if err := bot.UpgradeWebsocket(cli, c.Writer, c.Request); err != nil {
			fmt.Println("创建机器人失败", err)
		}
	})
	realPort, err := RunForwardGin(router, ":"+config.ForwardPort)
	if err != nil {
		for i := 9101; i <= 9120; i++ {
			config.ForwardPort = strconv.Itoa(i)
			realPort, err := RunForwardGin(router, ":"+config.ForwardPort)
			if err != nil {
				log.Warn(fmt.Errorf("failed to run gin, err: %+v", err))
				continue
			}
			config.ForwardPort = realPort
			log.Infof("端口号 %s", realPort)
			log.Infof(fmt.Sprintf("正向 WebSocket 地址为 ws://localhost:%s/onebot/ws", realPort))
			break
		}
	} else {
		config.ForwardPort = realPort
		log.Infof("端口号 %s", realPort)
		log.Infof(fmt.Sprintf("正向 WebSocket 地址为 ws://localhost:%s/onebot/ws", realPort))
	}
}

func RunForwardGin(engine *gin.Engine, port string) (string, error) {
	ln, err := net.Listen("tcp", port)
	if err != nil {
		return "", err
	}
	_, randPort, _ := net.SplitHostPort(ln.Addr().String())
	go func() {
		if err := http.Serve(ln, engine); err != nil {
			util.FatalError(fmt.Errorf("failed to serve http, err: %+v", err))
		}
	}()
	return randPort, nil
}

func PasswordLogin(uin uint32, password string) {
	set := config.ReadSetting()
	cli := client.NewClient(uin, password)
	log.Infof("开始初始化设备信息")
	newDeviceInfo := device.GetDevice(int64(uin))
	di, _ := json.Marshal(newDeviceInfo)
	log.Infof("设备信息 %+v", string(di))
	log.Infof("创建机器人 %+v", uin)
	appInfo := auth.AppList[set.Platform][set.AppVersion]
	cli.UseVersion(appInfo)
	cli.UseDevice(newDeviceInfo)
	cli.AddSignServer(set.SignServer)
	bot.Clients.Store(int64(uin), cli)
	log.Infof("登录中...")
	ok, err := LoginStatus(cli)
	if err != nil {
		// TODO 登录失败，是否需要删除？
		log.Errorf("failed to login, err: %+v", err)
		return
	}
	if ok {
		log.Infof("登录成功")
		AfterLogin(cli)
	} else {
		log.Infof("登录失败")
	}
}

func LoginStatus(cli *client.QQClient) (bool, error) {
	resp, err := cli.PasswordLogin()
	if err != nil {
		return false, err
	}
	if resp.Code == byte(45) {
		log.Warn("您的账号被限制登录，请配置 SignServer 后重试")
	}
	if resp.Code == byte(235) {
		log.Warn("设备信息被封禁，请删除设备（device）文件夹里对应设备文件后重试")
	}
	if resp.Code == byte(237) {
		log.Warn("登录过于频繁，请在手机QQ登录并根据提示完成认证")
	}
	ok, err := ProcessLoginResp(cli, resp)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func ProcessLoginResp(cli *client.QQClient, resp *client.LoginResponse) (bool, error) {
	if resp.Success {
		WaitingCaptchas.Delete(int64(cli.Uin))
		return true, nil
	}
	if resp.Error == client.SMSOrVerifyNeededError {
		if config.AllSetting.SMS {
			resp.Error = client.SMSNeededError
		} else {
			resp.Error = client.UnsafeDeviceError
		}
	}
	log.Infof("验证码处理页面: http://localhost:%s/dashcard", config.Port)
	log.Infof("验证码处理页面: 暂不支持处理")
	switch resp.Error {
	case client.SMSNeededError:
		log.Infof("遇到短信验证码，根据README提示操作 https://github.com/2mf8/Go-Lagrange-Client (顺便star)")
	case client.TooManySMSRequestError:
		log.Errorf("请求验证码太频繁")
	case client.NeedCaptcha:
		log.Infof("遇到图形验证码，遇到短信验证码，根据README提示操作 https://github.com/2mf8/Go-Lagrange-Client (顺便star)")
	case client.SliderNeededError:
		log.Infof("遇到滑块验证码，根据README提示操作https://github.com/2mf8/Go-Lagrange-Client (顺便star)")
	case client.UnsafeDeviceError:
		log.Infof("遇到设备锁扫码验证码，根据README提示操作 https://github.com/2mf8/Go-Lagrange-Client (顺便star)")
		log.Info("设置基础设置 SMS = true 可优先使用短信验证码")
	case client.OtherLoginError, client.UnknownLoginError:
		log.Errorf(resp.ErrorMessage)
		log.Warnf("登陆失败，建议开启/关闭设备锁后重试，或删除device-<QQ>.json文件后再次尝试")
		msg := resp.ErrorMessage
		if strings.Contains(msg, "版本") {
			log.Errorf("密码错误或账号被冻结")
		}
		if strings.Contains(msg, "上网环境") {
			log.Errorf("当前上网环境异常. 更换服务器并重试")
		}
		return false, fmt.Errorf("遇到不可处理的登录错误")
	}
	return false, fmt.Errorf("process login error")
}
