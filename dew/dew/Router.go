package dew

import (
	"net/http"
	"strings"
)

type router struct {
	roots    map[string]*node
	handlers map[string]HandlerFunction
}

func createRouter() *router {
	return &router{
		roots:    make(map[string]*node),
		handlers: make(map[string]HandlerFunction),
	}
}

func parsePattern(pattern string) []string {
	vs := strings.Split(pattern, "/")
	parts := make([]string, 0)
	for _, item := range vs {
		if item != "" {
			parts = append(parts, item)
			if item[0] == '*' {
				break
			}
		}
	}
	return parts
}

func (this *router) addRoute(method, pattern string, handler HandlerFunction) {
	parts := parsePattern(pattern)

	key := method + "-" + pattern
	_, ok := this.roots[method]
	if !ok {
		this.roots[method] = &node{}
	}
	this.roots[method].insert(pattern, parts, 0)
	this.handlers[key] = handler
}

func (this *router) getRoute(method, path string) (*node, map[string]string) {
	searchParts := parsePattern(path)
	params := make(map[string]string)
	root, ok := this.roots[method]

	if !ok {
		return nil, nil
	}

	n := root.search(searchParts, 0)
	if nil != n {
		parts := parsePattern(n.pattern)
		for index, part := range parts {
			if part[0] == ':' {
				params[part[1:]] = searchParts[index]
			}

			if part[0] == '*' && len(part) > 1 {
				params[part[1:]] = strings.Join(searchParts[index:], "/")
				break
			}
		}
		return n, params
	}
	return nil, nil
}

func (this *router) getRouters(method string) []*node {
	root, ok := this.roots[method]
	if !ok {
		return nil
	}
	nodes := make([]*node, 0)
	root.travel(nodes)
	return nodes
}

func (this *router) handle(context *Context) {
	n, params := this.getRoute(context.Method, context.Path)
	if n != nil {
		key := context.Method + "-" + n.pattern
		context.Params = params
		context.handlers = append(context.handlers, this.handlers[key])
	} else {
		context.handlers = append(context.handlers, func(context *Context) {
			context.WriteString(http.StatusNotFound, "404 NOT FOUND: %s\n", context.Path)
		})
	}
	context.Next()
}
