/*<喵喵公馆>专用软件

这是一个Minecraft服务器白名单管理系统，本程序会连接服务器Q群、MySQL数据库和Minecraft服务器。
系统启动后，在Q群内发送"MyID=XXX"的命令，机器人将自动将XXX添加至Minecraft服务器白名单中，
并同时从白名单中移除该Q号之前绑定的游戏ID。

启动时需要传入参数：
	go run main.go
		-db-user=数据库用户名
		-db-pswd=数据库密码
		-db-addr=数据库地址
		-rcon-addr=RCON服务地址
		-rcon-pswd=RCON服务密码
		-websocket-addr=酷Q-WebsocketAPI服务地址
		-websocket-bearer=酷Q-WebsocketAPI的access_token
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

	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/websocket"
	"github.com/micvbang/pocketmine-rcon"
)

//GroupID 是要接受消息的群号
const GroupID uint64 = 609632487

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

	groupMsg("白名单系统启动完成")
	for {
		var event map[string]interface{}
		reciveCoolQ(&event)

		switch event["post_type"] {
		case "message":
			if event["message_type"] == "group" &&
				uint64(event["group_id"].(float64)) == GroupID {
				var ID string
				msg := event["raw_message"].(string)
				if _, err := fmt.Sscanf(msg, "MyID=%s", &ID); err != nil {
					break
				}
				//玩家主动加白名
				qq := QQ(event["user_id"].(float64))

				if ID == idMatcher.FindString(ID) {
					setMyID(qq, ID)
				}
			}
		case "notice":
			log.Println(event)
			if event["notice_type"] == "group_decrease" &&
				uint64(event["group_id"].(float64)) == GroupID {
				//有人退群或被踢时要删除他的白名单
				qq := QQ(event["user_id"].(float64))
				if ID, ok, err := getIDbyQQ(qq); err != nil {
					log.Fatalf("查询QQ%d失败: %v", qq, err)
				} else if ok {
					whitelistRemove(ID)
					groupMsg(fmt.Sprintf("释放由 %v 申请的 %s", qq, ID))
				}
			}
		}
	}
}

//从酷Q接收消息
func reciveCoolQ(v interface{}) {
RETRY:
	err := ws.ReadJSON(v)
	if err != nil {
		log.Println("接收酷Q消息失败", err)
		//在这里尝试重连一下
		if err := openCoolQ(); err != nil {
			log.Fatal("重新连接酷Q失败", err)
		}
		goto RETRY
	}
}

//打开数据库
func openDatabase() (err error) {
	//open database
	log.Println("正在准备数据库")
	db, err = sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/miaoscraft_list", *dbUser, *dbPswd, *dbAddr))
	if err != nil {
		return
	}
	return
}

//连接mc服务器
func openRCON() (err error) {
	//open rcon
	log.Println("正在连接RCON")
	rconConn, err = rcon.NewConnection(*addrRCON, *pswdRCON)
	return
}

//连接酷Q
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
	selectID        *sql.Stmt
	selectQQ        *sql.Stmt
	addWhitelist    *sql.Stmt
	removeWhitelist *sql.Stmt
)

//编译SQL语句
func prepare() (err error) {
	if selectID, err = db.Prepare("SELECT QQ FROM `whitelist` WHERE ID=?"); err != nil {
		return
	}
	if selectQQ, err = db.Prepare("SELECT ID FROM `whitelist` WHERE QQ=?"); err != nil {
		return
	}
	if addWhitelist, err = db.Prepare("INSERT INTO `whitelist` (`QQ`, `ID`, `Time`) VALUES (?, ?, CURRENT_DATE())"); err != nil {
		return
	}
	if removeWhitelist, err = db.Prepare("DELETE FROM `whitelist` WHERE ID=?"); err != nil {
		return
	}
	return
}

func setMyID(qq QQ, ID string) {
	if dbQQ, ok, err := getQQbyID(ID); err != nil {
		log.Fatalf("查询ID%s失败: %v", ID, err)
	} else if ok && dbQQ != qq {
		//已经有其他人占用这个ID了
		groupMsg(fmt.Sprintf(" %s 占用着 %s, %s 没有得逞", dbQQ, ID, qq))
		return
	}

	if dbID, ok, err := getIDbyQQ(qq); err != nil {
		log.Fatalf("查询QQ%v失败: %v", qq, err)
	} else if ok {
		//取消该QQ之前绑定的ID的白名单
		whitelistRemove(dbID)
	}
	whitelistAdd(qq, ID)
}

var (
	db       *sql.DB
	ws       *websocket.Conn
	rconConn *rcon.Connection
)

func getIDbyQQ(qq QQ) (ID string, has bool, err error) {
	var rows *sql.Rows
	if rows, err = selectQQ.Query(uint64(qq)); err != nil {
		return
	} else if rows.Next() {
		has = true
		err = rows.Scan(&ID)
	}
	return
}

func getQQbyID(ID string) (qq QQ, has bool, err error) {
	var rows *sql.Rows
	if rows, err = selectID.Query(ID); err != nil {
		return
	} else if rows.Next() {
		has = true
		err = rows.Scan(&qq)
	}
	return
}

func whitelistRemove(ID string) {
	log.Println("删除白名单", ID)
	//删除数据库数据
	if _, err := removeWhitelist.Exec(ID); err != nil {
		log.Fatal(err)
	}
	//删除服务器端白名单
RETRY:
	if res, err := rconConn.SendCommand("whitelist remove " + ID); err != nil {
		log.Println("删除白名单失败", err)
		//这里尝试重现连接RCON
		if err := openRCON(); err != nil {
			log.Fatalf("连接RCON失败: %v\n", err)
		}
		goto RETRY
	} else {
		log.Println(res)
		groupMsg(fmtFliter.ReplaceAllString(res, ""))
	}
}

func whitelistAdd(qq QQ, ID string) {
	log.Println("添加白名单", qq, ID)
	//更新数据库
	if _, err := addWhitelist.Exec(uint64(qq), ID); err != nil {
		log.Fatal(err)
	}
	//向服务器提交白名单
RETRY:
	if res, err := rconConn.SendCommand("whitelist add " + ID); err != nil {
		log.Println("添加白名单失败,重试", err)
		//这里尝试重新连接RCON
		if err := openRCON(); err != nil {
			log.Fatalf("连接RCON失败: %v\n", err)
		}
		goto RETRY
	} else {
		log.Println(res)
		groupMsg(fmtFliter.ReplaceAllString(res, ""))
	}
}

var fmtFliter, _ = regexp.Compile("§.")

func groupMsg(msg string) {
	type params struct {
		GroupID uint64 `json:"group_id"`
		Message string `json:"message"`
		AutoEsc bool   `json:"auto_escape"`
	}
RETRY:
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
		goto RETRY
	}
}

//QQ defines a QQ number
type QQ uint64

func (qq QQ) String() string {
	return fmt.Sprintf("[CQ:at,qq=%d]", qq)
	// return strconv.FormatUint(uint64(qq), 10)
}
