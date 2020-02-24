package main

import (
	"dew/dew"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"
)

func testGroup() {
	server := dew.CreateEngine()

	server.GET("/index", func(context *dew.Context) {
		context.WriteHTML(http.StatusOK, "", `<h1>Index Page</h1>`)
	})

	v1 := server.Group("/v1")
	{
		v1.GET("/", func(context *dew.Context) {
			context.WriteHTML(http.StatusOK, "", `<h1>V1 Page</h1>`)
		})

		v1.GET("/hello", func(context *dew.Context) {
			context.WriteString(http.StatusOK, "hello %s, you're at %s\n", context.Query("name"), context.Path)
		})
	}

	v2 := server.Group("/v2")
	{
		v2.GET("/hello/:name", func(context *dew.Context) {
			context.WriteString(http.StatusOK, "hello %s, you're at %s\n", context.Param("name"), context.Path)
		})

		v2.POST("/login", func(context *dew.Context) {
			context.WriteJson(http.StatusOK, dew.H{
				"username": context.PostForm("username"),
				"password": context.PostForm("password"),
			})
		})
	}

	server.Run(":8888")
}

func onlyForV2() dew.HandlerFunction {
	return func(context *dew.Context) {
		// Start timer
		t := time.Now()
		// if a server error occurred
		context.WriteString(500, "", "Internal Server Error")
		// Calculate resolution time
		log.Printf("[%d] %s in %v for group v2", context.Code, context.Request.RequestURI, time.Since(t))
	}
}

func testMiddleware() {
	server := dew.CreateEngine()
	server.Use(dew.Logger())
	server.GET("/", func(context *dew.Context) {
		context.WriteHTML(http.StatusOK, "", "<h1>Hello dew</h1>")
	})

	v2 := server.Group("/v2")
	v2.Use(onlyForV2())
	{
		v2.GET("/hello/:name", func(context *dew.Context) {
			context.WriteString(http.StatusOK, "hello %s, you're at %s\n", context.Param("name"), context.Path)
		})
	}

	server.Run(":8888")
}

type student struct {
	Name string
	Age  uint8
}

func formatAsData(t time.Time) string {
	y, m, d := t.Date()
	return fmt.Sprintf("%d-%02d-%02d", y, m, d)
}

func testTemplate() {
	server := dew.CreateEngine()
	server.Use(dew.Logger())
	server.SetFunctionMap(template.FuncMap{
		"formatAsData": formatAsData,
	})
	server.LoadHTMLGlob("templates/*")
	server.Static("/assets", "./static")

	stu1 := &student{
		Name: "Geek", Age: 20,
	}

	stu2 := &student{
		Name: "Jack", Age: 22,
	}

	server.GET("/", func(context *dew.Context) {
		context.WriteHTML(http.StatusOK, "css.tmpl", nil)
	})

	server.GET("/studens", func(context *dew.Context) {
		context.WriteHTML(http.StatusOK, "arr.tmpl", dew.H{
			"title": "dew",
			"stus":  [...]*student{stu1, stu2},
		})
	})

	server.GET("/date", func(context *dew.Context) {
		context.WriteHTML(http.StatusOK, "custom_func.tmpl", dew.H{
			"title": "dew",
			"now":   time.Date(2018, 8, 17, 0, 0, 0, 0, time.UTC),
		})
	})

	server.Run(":8888")
}

func testRecovery() {
	server := dew.Default()
	server.GET("/", func(context *dew.Context) {
		context.WriteString(http.StatusOK, "Hello\n")
	})

	server.GET("/panic", func(context *dew.Context) {
		names := []string{
			"Hello",
		}
		context.WriteString(http.StatusOK, names[100])
	})

	server.Run(":8888")
}

func main() {
	testRecovery()
}
