package dew

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type (
	H map[string]interface{}

	Context struct {
		//来源
		Writer  http.ResponseWriter
		Request *http.Request
		//请求
		Path   string
		Method string
		Params map[string]string
		//响应
		Code int
		//中间件
		handlers []HandlerFunction
		index    int
		engine   *Engine
	}
)

func CreateContext(writer http.ResponseWriter, request *http.Request) *Context {
	return &Context{
		Path:    request.URL.Path,
		Method:  request.Method,
		Writer:  writer,
		Request: request,
		index:   -1,
	}
}

func (this *Context) Next() {
	this.index++
	length := len(this.handlers)
	for ; this.index < length; this.index++ {
		this.handlers[this.index](this)
	}
}

func (this *Context) Fail(code int, err string) {
	this.index = len(this.handlers)
	this.WriteJson(code, H{
		"code":    code,
		"message": err,
	})
}

func (this *Context) PostForm(key string) string {
	return this.Request.FormValue(key)
}

func (this *Context) Query(key string) string {
	return this.Request.URL.Query().Get(key)
}

func (this *Context) Param(key string) string {
	value, _ := this.Params[key]
	return value
}

func (this *Context) SetCode(code int) {
	this.Code = code
	this.Writer.WriteHeader(code)
}

func (this *Context) SetHeader(key, value string) {
	this.Writer.Header().Set(key, value)
}

func (this *Context) WriteString(code int, format string, values ...interface{}) {
	this.SetCode(code)
	this.SetHeader("Content-Type", "text/plain")
	this.Writer.Write([]byte(fmt.Sprintf(format, values...)))
}

func (this *Context) WriteJson(code int, object interface{}) {
	this.SetCode(code)
	this.SetHeader("Content-Type", "application/json")
	encoder := json.NewEncoder(this.Writer)
	if err := encoder.Encode(object); nil != err {
		http.Error(this.Writer, err.Error(), http.StatusInternalServerError)
	}
}

func (this *Context) WriteData(code int, data []byte) {
	this.SetCode(code)
	this.Writer.Write(data)
}

func (this *Context) WriteHTML(code int, name string, data interface{}) {
	this.SetCode(code)
	this.SetHeader("Content-Type", "text/html")
	if err := this.engine.htmlTemplates.ExecuteTemplate(this.Writer, name, data); err != nil {
		this.Fail(http.StatusInternalServerError, err.Error())
	}
}
