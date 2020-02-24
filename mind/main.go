package main

import (
	"mind/controller"
	"net/http"
)
func main(){
	//设置处理静态资源，如css和js文件
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("views/static"))))
	//直接去html页面
	http.Handle("/templates/", http.StripPrefix("/templates/", http.FileServer(http.Dir("views/templates"))))
	http.HandleFunc("/",controller.Login)
	http.HandleFunc("/admin/dashboard",controller.AdminDashBoard)
	http.HandleFunc("/admin/tables",controller.AdminTables)


	http.ListenAndServe(":8000",nil)
}