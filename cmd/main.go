/*<喵喵公馆>专用软件

 */
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/Tnze/go-mc/bot"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/micvbang/pocketmine-rcon"
)

const (
	//GroupID 是要接受消息的群号
	GroupID = 609632487
	//ServerIP 是Ping服务器地址
	ServerIP = "play.miaoscraft.cn"
)

var (
	dbUser = flag.String("db-user", "", "Mysql数据库用户名")
	dbPswd = flag.String("db-pswd", "", "Mysql数据库密码")
	dbAddr = flag.String("db-addr", "", "Mysql数据库地址")

	addrRCON = flag.String("rcon-addr", "", "RCON地址")
	pswdRCON = flag.String("rcon-pswd", "", "RCON密码")

	wsAddr   = flag.String("websocket-addr", "", "酷Q WebsocketAPI地址")
	wsBearer = flag.String("websocket-bearer", "", "酷Q WebsocketAPI验证码")
	httpAddr = flag.String("http-addr", "", "酷Q HttpAPI地址")

	idMatcher, _ = regexp.Compile(`\w{3,16}`)
)

func main() {
	log.Println("喵喵公馆专用")
	flag.Parse()

	if err := openDatabase(); err != nil {
		log.Fatal("打开数据库失败", err)
	}
	if err := prepare(); err != nil {
		log.Fatal("准备查询语句失败", err)
	}
	if err := openRCON(); err != nil {
		log.Fatal("连接RCON失败", err)
	}
	if err := getLoginInfo(); err != nil {
		log.Fatal("获取酷Q登录号失败", err)
	}
	if err := openCoolQ(); err != nil {
		log.Fatal("连接酷Q失败", err)
	}
	for {
		var event map[string]interface{}
		reciveCoolQ(&event)

		switch event["post_type"] {
		case "message":
			if event["message_type"] == "group" ||
				event["group_id"] == GroupID {
				QQ := uint64(event["user_id"].(float64))
				msg := event["raw_message"].(string)
				pf := fmt.Sprintf("[CQ:at,qq=%d] :", botQQ)
				if strings.HasPrefix(msg, pf) {
					command(QQ, msg[len(pf):])
				}
			}
		}
	}
}

func reciveCoolQ(v interface{}) {
	err := ws.ReadJSON(v)
	if err != nil {
		log.Println("接收酷Q消息失败", err)
		//在这里尝试重连一下
		if err := openCoolQ(); err != nil {
			log.Fatal("重新连接酷Q失败", err)
		}
	}
}

func openDatabase() (err error) {
	//open database
	log.Println("正在准备数据库")
	db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/miaoscraft_list", *dbUser, *dbPswd, *dbAddr))
	if err != nil {
		return
	}
	return
}

func openRCON() (err error) {
	//open rcon
	log.Println("正在连接RCON")
	rconConn, err = rcon.NewConnection(*addrRCON, *pswdRCON)
	if err != nil {
		return
	}
	return
}

func openCoolQ() (err error) {
	log.Println("正在连接酷Q")
	URL := &url.URL{
		Scheme: "ws",
		Host:   *wsAddr,
	}

	Header := http.Header{
		"Authorization": []string{"Bearer " + *wsBearer},
	}

	//连接event接口
	ws, _, err = websocket.DefaultDialer.Dial(URL.String(), Header)
	return
}

func prepare() (err error) {
	if selectQQ, err = db.Prepare("SELECT PermissionLevel FROM `magicians` WHERE QQ=?"); err != nil {
		return
	}
	return
}

var (
	selectQQ *sql.Stmt
	db       *sql.DB
	ws       *websocket.Conn
	rconConn *rcon.Connection

	botQQ int64
)

func sendMsg(msg string) {
	type params struct {
		GroupID uint64 `json:"group_id"`
		Message string `json:"message"`
		AutoEsc bool   `json:"auto_escape"`
	}

	if err := ws.WriteJSON(struct {
		Action string `json:"action"`
		Params params `json:"params"`
	}{
		Action: "send_group_msg",
		Params: params{
			GroupID: GroupID,
			Message: msg,
		},
	}); err != nil {
		log.Println("发送酷Q消息失败", err)
		//在这里尝试重连一下
		if err := openCoolQ(); err != nil {
			log.Fatal("重新连接酷Q失败", err)
		}
	}
}

func getLoginInfo() error {
	URL := &url.URL{
		Scheme: "http",
		Host:   *httpAddr,
		Path:   "get_login_info",
	}
	req, err := http.NewRequest("GET", URL.String(), nil)
	if err != nil {
		return err
	}
	req.Header = http.Header{
		"Authorization": []string{"Bearer " + *wsBearer},
	}

	resp, err := new(http.Client).Do(req)
	if err != nil {
		return err
	}

	var respData struct {
		ErrorCode int `json:"retcode"`
		Data      struct {
			ID       int64  `json:"user_id"`
			NickName string `json:"nickname"`
		} `json:"data"`
	}
	err = json.NewDecoder(resp.Body).Decode(&respData)
	if err != nil {
		return err
	}
	if respData.ErrorCode != 0 {
		return fmt.Errorf("酷Q API调用出错: %d", respData.ErrorCode)
	}

	log.Println("机器人QQ:", respData.Data.ID)
	log.Println("机器人昵称:", respData.Data.NickName)

	botQQ = respData.Data.ID
	return nil
}

//从数据库读取用户的等级，默认为0
func getLevel(QQ uint64) (level int) {
	if rows, err := selectQQ.Query(QQ); err != nil {
		log.Fatal(err)
	} else if rows.Next() {
		err := rows.Scan(&level)
		if err != nil {
			log.Fatal(err)
		}
	}
	return
}

var fmtFliter, _ = regexp.Compile("§.")

func command(QQ uint64, msg string) {
	level := getLevel(QQ)
	switch {
	case strings.HasPrefix(msg, "mcmd:"):
		if level >= 90 {
			log.Println(QQ, msg)
			msg := msg[len("sudo:"):]
			rconCmd(msg)
		} else {
			sendMsg(invokedMsg(level, 90))
		}
	case strings.HasPrefix(msg, "info:"):
		if level >= 30 {
			sendMsg(checkInfo(msg[5:]))
		} else {
			sendMsg(invokedMsg(level, 30))
		}
	case strings.HasPrefix(msg, "list"):
		if level >= 0 {
			rconCmd("list")
		} else {
			sendMsg(invokedMsg(level, 0))
		}
	case strings.HasPrefix(msg, "ping"):
		if level >= 0 {
			ping()
		} else {
			sendMsg(invokedMsg(level, 0))
		}
	}
}

func invokedMsg(level, need int) string {
	return fmt.Sprintf("invoking rejected, your permission level %d, need %d", level, need)
}

func checkInfo(cmd string) string {
	if strings.HasPrefix(cmd, "name:") {
		qq, has, err := getQQbyID(cmd[5:])
		if err != nil {
			return err.Error()
		} else if !has {
			return "none"
		} else {
			return fmt.Sprintf("[CQ:at,qq=%d]", qq)
		}
	}
	var qq uint64
	if n, err := fmt.Sscanf(cmd, "[CQ:at,qq=%d]", &qq); n == 1 || err == nil {
		id, has, err := getIDbyQQ(qq)
		if err != nil {
			return err.Error()
		} else if !has {
			return "none"
		} else {
			return id
		}
	}
	return ""
}

func getIDbyQQ(qq uint64) (ID string, has bool, err error) {
	var rows *sql.Rows
	if rows, err = db.Query("SELECT ID FROM `whitelist` WHERE QQ=?", qq); err != nil {
		return
	} else if rows.Next() {
		has = true
		err = rows.Scan(&ID)
	}
	return
}

func getQQbyID(ID string) (qq uint64, has bool, err error) {
	var rows *sql.Rows
	if rows, err = db.Query("SELECT QQ FROM `whitelist` WHERE ID=?", ID); err != nil {
		return
	} else if rows.Next() {
		has = true
		err = rows.Scan(&qq)
	}
	return
}

func rconCmd(cmd string) {
RETRY:
	if res, err := rconConn.SendCommand(cmd); err != nil {
		log.Println("添加白名单失败", err)
		//reopen RCON
		if err := openRCON(); err != nil {
			log.Fatalf("连接RCON失败: %v\n", err)
		}
		goto RETRY //retry
	} else {
		res = strings.Trim(res, " \n")
		log.Println(res)
		if res != "" {
			sendMsg(fmtFliter.ReplaceAllString(res, ""))
		}
	}
}

func ping() {
	respjson, delay, err := bot.PingAndList(ServerIP, 25565)
	if err != nil {
		sendMsg("Ping fail: " + err.Error())
		return
	}

	var resp struct {
		Version struct {
			Name     string `json:"name"`
			Protocol int    `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
			Sample []struct {
				Name string `json:"name"`
				ID   string `json:"id"`
			} `json:"sample"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
		Delay time.Duration `json:"-"`
	}
	resp.Delay = delay
	if err := json.Unmarshal(respjson, &resp); err != nil {
		sendMsg("Ping fail: " + err.Error())
		return
	}

	var respStr strings.Builder
	if err := pingResp.Execute(&respStr, resp); err != nil {
		sendMsg("Ping fail: " + err.Error())
		return
	}
	sendMsg(respStr.String())
}

var pingResp = template.Must(template.New("pingResp").Parse(
	`服务器:{{ .Version.Name }}
{{ .Description.Text }}
延迟:{{ .Delay }}
人数:{{ .Players.Online }}/{{ .Players.Max }}{{ range .Players.Sample }}
[{{ .Name }}]{{ end }}`,
))
