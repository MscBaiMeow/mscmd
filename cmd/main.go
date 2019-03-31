/*<喵喵公馆>专用软件

 */
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/micvbang/pocketmine-rcon"
)

//GroupID 是要接受消息的群号
const GroupID = 609632487

var dbUser = flag.String("db-user", "", "Mysql数据库用户名")
var dbPswd = flag.String("db-pswd", "", "Mysql数据库密码")
var dbAddr = flag.String("db-addr", "", "Mysql数据库地址")

var addrRCON = flag.String("rcon-addr", "", "RCON地址")
var pswdRCON = flag.String("rcon-pswd", "", "RCON密码")

var wsAddr = flag.String("websocket-addr", "", "酷Q WebsocketAPI地址")
var wsBearer = flag.String("websocket-bearer", "", "酷Q WebsocketAPI验证码")

var idMatcher, _ = regexp.Compile(`\w{3,16}`)

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
				if strings.HasPrefix(msg, "msc:") {
					command(QQ, msg[len("msc:"):])
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

	ws, _, err = websocket.DefaultDialer.Dial(URL.String(), Header)
	return
}

var (
	selectQQ *sql.Stmt
)

func prepare() (err error) {
	if selectQQ, err = db.Prepare("SELECT PermissionLevel FROM `magicians` WHERE QQ=?"); err != nil {
		return
	}
	return
}

var (
	db       *sql.DB
	ws       *websocket.Conn
	rconConn *rcon.Connection
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
	if strings.HasPrefix(msg, "sudo:") ||
		strings.HasPrefix(msg, "rcon:") {
		if level >= 90 {
			log.Println(QQ, msg)
			msg := msg[5:]
			rconCmd(msg)
		} else {
			sendMsg(invokedMsg(level, 90))
		}
	} else if strings.HasPrefix(msg, "list") {
		if level >= 0 {
			rconCmd("list")
		} else {
			sendMsg(invokedMsg(level, 0))
		}
	}
}

func invokedMsg(level, need int) string {
	return fmt.Sprintf("invoking rejected, your permission level %d, need %d", level, need)
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
