package dew

import (
	"html/template"
	"net/http"
	"strings"
)

type (
	HandlerFunction func(*Context)

	Engine struct {
		*RouterGroup
		router *router
		groups []*RouterGroup
		//对html渲染
		htmlTemplates *template.Template
		functionMap   template.FuncMap
	}
)

func CreateEngine() *Engine {
	engine := &Engine{
		router: createRouter(),
	}
	engine.RouterGroup = &RouterGroup{
		engine: engine,
	}
	engine.groups = []*RouterGroup{engine.RouterGroup}
	return engine
}

func Default() *Engine {
	engine := CreateEngine()
	engine.Use(Logger(), Recovery())
	return engine
}

//自定义渲染函数
func (this *Engine) SetFunctionMap(functionMap template.FuncMap) {
	this.functionMap = functionMap
}

func (this *Engine) LoadHTMLGlob(pattern string) {
	this.htmlTemplates = template.Must(template.New("").Funcs(this.functionMap).ParseGlob(pattern))
}

func (this *Engine) addRoute(method, pattern string, handler HandlerFunction) {
	this.router.addRoute(method, pattern, handler)
}

func (this *Engine) GET(pattern string, handler HandlerFunction) {
	this.addRoute("GET", pattern, handler)
}

func (this *Engine) POST(pattern string, handler HandlerFunction) {
	this.addRoute("POST", pattern, handler)
}

//Run 定义了启动http服务器的方法
func (this *Engine) Run(host string) error {
	return http.ListenAndServe(host, this)
}

func (this *Engine) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	var middlewares []HandlerFunction
	for _, group := range this.groups {
		if strings.HasPrefix(request.URL.Path, group.prefix) {
			middlewares = append(middlewares, group.middlewares...)
		}
	}
	context := CreateContext(writer, request)
	context.handlers = middlewares
	context.engine = this
	this.router.handle(context)
}
