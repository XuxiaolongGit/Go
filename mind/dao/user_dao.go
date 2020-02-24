package dao

import (
	"mind/model"
	"mind/utils"
)

func CheckUserNameAndPassWord(username,password string)(*model.User,error){
	//写sql语句
	sqlStr := "select id,username,password,qq from where username = ? and password =?"
	//执行
	row := utils.Db.QueryRow(sqlStr,username,password)
	user := &model.User{}
	row.Scan(&user.ID,&user.Username,&user.Password,&user.QQ)
	return user,nil
}

func CheckUserName(username string)([]*model.User,error){
	//写sql语句
	sqlStr := "select * from users where username = ?"
	//执行
	rows,err := utils.Db.Query(sqlStr,username)
	if err != nil{
		return nil,err
	}
	var users []*model.User
	for rows.Next(){
		user := &model.User{}
		err2 := rows.Scan(&user.ID,&user.Username,&user.Password,&user.QQ)
		if err2 != nil{
			return nil,err2
		}
		users = append(users,user)
	}
	return users,nil
}

//SaveUser  向数据库插入 