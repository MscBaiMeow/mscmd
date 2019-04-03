package main

/*
	检查数据库：
	补齐每个条目的UUID
	更新玩家的ID
*/
import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/miaoscraft/mojang"
	"github.com/satori/go.uuid"
)

var dbUser = flag.String("db-user", "", "Mysql数据库用户名")
var dbPswd = flag.String("db-pswd", "", "Mysql数据库密码")
var dbAddr = flag.String("db-addr", "", "Mysql数据库地址")

func main() {
	flag.Parse()
	log.Println(*dbAddr, *dbPswd, *dbUser)
	log.Println("正在准备数据库")
	db, err := sql.Open("mysql", fmt.Sprintf("%s:%s@tcp(%s)/miaoscraft_list?parseTime=true", *dbUser, *dbPswd, *dbAddr))
	if err != nil {
		log.Fatal(err)
	}

	list, err := db.Query("SELECT ID,UUID,Time FROM `whitelist`")
	if err != nil {
		log.Fatal(err)
	}
	var id string
	var UUID []byte
	var t time.Time
	var wait sync.WaitGroup
	for list.Next() {
		if err := list.Scan(&id, &UUID, &t); err != nil {
			log.Fatal(err)
		}
		//查询UUID
		wait.Add(1)
		go func(id string, t time.Time) {
			defer wait.Done()

			nu, err := mojang.GetUUID(id, t)
			if err != nil {
				log.Printf("检查%s失败: %v\n", id, err)
				return
			}
			log.Println(nu)
			if nu.Name != id {
				log.Println("警告:", nu.Name, id)
			}
			u, err := uuid.FromString(nu.UUID)
			if err != nil {
				log.Fatalf("解析UUID: %s失败: %v", nu.UUID, err)
			}
			_, err = db.Exec("UPDATE whitelist SET UUID=?, ID=?, Time=CURRENT_DATE() WHERE ID=?", u.Bytes(), nu.Name, id)
			if err != nil {
				log.Fatal("更新数据失败", err)
			}
		}(id, t)
	}
	wait.Wait()
}
