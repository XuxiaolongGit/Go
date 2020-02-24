package controller

import (
	"net/http"
	"text/template"
)

func AdminDashBoard(w http.ResponseWriter,r *http.Request){
		//用户名和密码正确
		t := template.Must(template.ParseFiles("views/templates/admin/dashboard.html"))
		t.Execute(w,"")

}
func AdminTables(w http.ResponseWriter,r *http.Request){
	//用户名和密码正确
	t := template.Must(template.ParseFiles("views/templates/admin/tables.html"))
	t.Execute(w,"")

}