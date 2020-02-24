package dew

import (
	"log"
	"net/http"
	"path"
)

//分组
type RouterGroup struct {
	prefix      string
	middlewares []HandlerFunction
	parent      *RouterGroup
	engine      *Engine
}

func (this *RouterGroup) Group(prefix string) *RouterGroup {
	engine := this.engine
	group := &RouterGroup{
		prefix: this.prefix + prefix,
		parent: this,
		engine: engine,
	}
	engine.groups = append(engine.groups, group)
	return group
}

func (this *RouterGroup) addRoute(method, comp string, handler HandlerFunction) {
	pattern := this.prefix + comp
	log.Printf("Route %4s - %4s", method, pattern)
	this.engine.router.addRoute(method, pattern, handler)
}

func (this *RouterGroup) Use(middlewares ...HandlerFunction) {
	this.middlewares = append(this.middlewares, middlewares...)
}

func (this *RouterGroup) GET(pattern string, handler HandlerFunction) {
	this.addRoute("GET", pattern, handler)
}

func (this *RouterGroup) POST(pattern string, handler HandlerFunction) {
	this.addRoute("POST", pattern, handler)
}

//createStaticHandler 创建静态文件处理器
func (this *RouterGroup) createStaticHandler(relativePath string, fs http.FileSystem) HandlerFunction {
	absolutePath := path.Join(this.prefix, relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))
	return func(context *Context) {
		file := context.Param("filepath")
		//检查文件是否存在和/或我们是否有权限访问它
		if _, err := fs.Open(file); nil != err {
			context.SetCode(http.StatusNotFound)
			return
		}
		fileServer.ServeHTTP(context.Writer, context.Request)
	}
}

//Static 静态文件服务
func (this *RouterGroup) Static(relativePath, root string) {
	handler := this.createStaticHandler(relativePath, http.Dir(root))
	urlPattern := path.Join(relativePath, "/*filepath")
	//注册 GET 处理器
	this.GET(urlPattern, handler)
}
