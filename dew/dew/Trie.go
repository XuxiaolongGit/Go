package dew

import "strings"

type node struct {
	pattern  string  //待匹配路由
	part     string  //路由中的一部分
	children []*node //子节点
	isWild   bool    //是否精确匹配
}

func (this *node) travel(list []*node) {
	if this.pattern != "" {
		list = append(list, this)
	}

	for _, child := range this.children {
		child.travel(list)
	}
}

//第一个匹配成功的节点,用于插入
func (this *node) matchChild(part string) *node {
	for _, child := range this.children {
		if child.part == part || child.isWild {
			return child
		}
	}
	return nil
}

//所有匹配成功的节点,用于查找
func (this *node) matchChildren(part string) []*node {
	nodes := make([]*node, 0)
	for _, child := range this.children {
		if child.part == part || child.isWild {
			nodes = append(nodes, child)
		}
	}
	return nodes
}

func (this *node) insert(pattern string, parts []string, height int) {
	if len(parts) == height {
		this.pattern = pattern
		return
	}

	part := parts[height]
	child := this.matchChild(part)
	if nil == child {
		child = &node{
			part:   part,
			isWild: part[0] == ':' || part[0] == '*',
		}
		this.children = append(this.children, child)
	}
	child.insert(pattern, parts, height+1)
}

func (this *node) search(parts []string, height int) *node {
	if len(parts) == height || strings.HasPrefix(this.part, "*") {
		if this.pattern == "" {
			return nil
		}
		return this
	}

	part := parts[height]
	children := this.matchChildren(part)
	for _, child := range children {
		result := child.search(parts, height+1)
		if nil != result {
			return result
		}
	}
	return nil
}
