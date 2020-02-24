package controller

import (
	"fmt"
	"mind/dao"
	"net/http"
	"text/template"
)

func Login(w http.ResponseWriter,r *http.Request){
	//获取用户名和密码
	username := r.PostFormValue("username")
	password := r.PostFormValue("password")
	user,_:=dao.CheckUserNameAndPassWord(username,password)
	fmt.Println("获取的User是",user)
	if user.ID >0 {
		//用户名和密码正确
		t := template.Must(template.ParseFiles("views/index.html"))
		t.Execute(w,"")
	}else{
		//用户名和密码不正确
		t := template.Must(template.ParseFiles("views/index.html"))
		t.Execute(w,"")
	}
}