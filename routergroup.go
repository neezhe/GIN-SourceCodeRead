// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"net/http"
	"path"
	"regexp"
	"strings"
)

// IRouter defines all router handle interface includes single and group router.
type IRouter interface {
	IRoutes
	Group(string, ...HandlerFunc) *RouterGroup
}

// Iroutes defins all router handle interface.
type IRoutes interface {  //要实现全部才能成为相同的interface
	Use(...HandlerFunc) IRoutes

	Handle(string, string, ...HandlerFunc) IRoutes
	Any(string, ...HandlerFunc) IRoutes
	GET(string, ...HandlerFunc) IRoutes
	POST(string, ...HandlerFunc) IRoutes
	DELETE(string, ...HandlerFunc) IRoutes
	PATCH(string, ...HandlerFunc) IRoutes
	PUT(string, ...HandlerFunc) IRoutes
	OPTIONS(string, ...HandlerFunc) IRoutes
	HEAD(string, ...HandlerFunc) IRoutes

	StaticFile(string, string) IRoutes
	Static(string, string) IRoutes
	StaticFS(string, http.FileSystem) IRoutes
}

// RouterGroup is used internally to configure router, a RouterGroup is associated with
// a prefix and an array of handlers (middleware).
type RouterGroup struct {
	Handlers HandlersChain  //midleware表示的函数会填进来
	basePath string  //路由路径，相对于子路由的上级路径
	engine   *Engine  //父节点路由的Engine实体
	root     bool  //是否为根节点路由
}

var _ IRouter = &RouterGroup{}

// Use adds middleware to the group, see example code in github.
func (group *RouterGroup) Use(middleware ...HandlerFunc) IRoutes {
	group.Handlers = append(group.Handlers, middleware...)
	return group.returnObj()
}

// Group creates a new router group. You should add all the routes that have common middlwares or the same path prefix.
// For example, all the routes that use a common middlware for authorization could be grouped.
//一个engin有一个RouterGroup,调用此函数之前就是使用的是root为true，base path为"/"的RouterGroup，调用此函数后，变成了root为false，base path为"/"+relative path的RouterGroup
//也就是说此时一个engin被两个RouterGroup包含着了。
func (group *RouterGroup) Group(relativePath string, handlers ...HandlerFunc) *RouterGroup { //在加一个统一前缀的时候v1 := router.Group("/v1")
	//注意此处是根据已有的group新建了一个RouterGroup
	return &RouterGroup{ //它把RouterGroup暴露出来，而不是把Engine暴露出来，这样整个逻辑就很清晰，我可以对这个RouterGroup进行各种自定义方法。在最后调用v1.GET的时候再将带有绝对路径的handler挂在engine上的tree上。
		Handlers: group.combineHandlers(handlers),
		basePath: group.calculateAbsolutePath(relativePath),
		engine:   group.engine,
	}
}

// BasePath returns the base path of router group.
// For example, if v := router.Group("/rest/n/v1/api"), v.BasePath() is "/rest/n/v1/api".
func (group *RouterGroup) BasePath() string {
	return group.basePath
}

func (group *RouterGroup) handle(httpMethod, relativePath string, handlers HandlersChain) IRoutes {
	absolutePath := group.calculateAbsolutePath(relativePath) //1.取得监听器（handler函数）所监听路由的绝对完整路径,基本路径加上相对路径
	//下面的操作就导致了下面的变量handlers中为[中间件函数，noRouter函数,路由对应的处理函数]
	handlers = group.combineHandlers(handlers) //2.将group也就是父类下所有的监听器（handler函数）和我们传进来的函数指针合并返回后放到handlers中，应该是添加了中间件的那些函数
	group.engine.addRoute(httpMethod, absolutePath, handlers)//3.将所得到的信息（包括所有的函数指针）添加到父节点中，并返回。把新路由加入树中，每个方法是一个树的根节点
	return group.returnObj()
}

// Handle registers a new request handle and middleware with the given path and method.
// The last handler should be the real handler, the other ones should be middleware that can and should be shared among different routes.
// See the example code in github.
//
// For GET, POST, PUT, PATCH and DELETE requests the respective shortcut
// functions can be used.
//
// This function is intended for bulk loading and to allow the usage of less
// frequently used, non-standardized or custom methods (e.g. for internal
// communication with a proxy).
func (group *RouterGroup) Handle(httpMethod, relativePath string, handlers ...HandlerFunc) IRoutes {
	if matches, err := regexp.MatchString("^[A-Z]+$", httpMethod); !matches || err != nil {
		panic("http method " + httpMethod + " is not valid")
	}
	return group.handle(httpMethod, relativePath, handlers)
}

// POST is a shortcut for router.Handle("POST", path, handle).
func (group *RouterGroup) POST(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("POST", relativePath, handlers)
}

// GET is a shortcut for router.Handle("GET", path, handle). //HandlerFunc是一个函数指针，显然调用的是父类的GET方法。
func (group *RouterGroup) GET(relativePath string, handlers ...HandlerFunc) IRoutes { //...HandlerFunc表示的就是[]HandlerFunc，一个意思
	return group.handle("GET", relativePath, handlers) ///gin还定义了接口IRoutes来表示RouterGroup实现的方法。
	//首先我们已经把request和responseWriter封装在context里面了，多个handler只要处理好这个context就可以了，所以是可以一个路由拥有多个handler的。
}

// DELETE is a shortcut for router.Handle("DELETE", path, handle).
func (group *RouterGroup) DELETE(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("DELETE", relativePath, handlers)
}

// PATCH is a shortcut for router.Handle("PATCH", path, handle).
func (group *RouterGroup) PATCH(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("PATCH", relativePath, handlers)
}

// PUT is a shortcut for router.Handle("PUT", path, handle).
func (group *RouterGroup) PUT(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("PUT", relativePath, handlers)
}

// OPTIONS is a shortcut for router.Handle("OPTIONS", path, handle).
func (group *RouterGroup) OPTIONS(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("OPTIONS", relativePath, handlers)
}

// HEAD is a shortcut for router.Handle("HEAD", path, handle).
func (group *RouterGroup) HEAD(relativePath string, handlers ...HandlerFunc) IRoutes {
	return group.handle("HEAD", relativePath, handlers)
}

// Any registers a route that matches all the HTTP methods.
// GET, POST, PUT, PATCH, HEAD, OPTIONS, DELETE, CONNECT, TRACE.
func (group *RouterGroup) Any(relativePath string, handlers ...HandlerFunc) IRoutes {
	group.handle("GET", relativePath, handlers)
	group.handle("POST", relativePath, handlers)
	group.handle("PUT", relativePath, handlers)
	group.handle("PATCH", relativePath, handlers)
	group.handle("HEAD", relativePath, handlers)
	group.handle("OPTIONS", relativePath, handlers)
	group.handle("DELETE", relativePath, handlers)
	group.handle("CONNECT", relativePath, handlers)
	group.handle("TRACE", relativePath, handlers)
	return group.returnObj()
}

// StaticFile registers a single route in order to serve a single file of the local filesystem.
// router.StaticFile("favicon.ico", "./resources/favicon.ico")
func (group *RouterGroup) StaticFile(relativePath, filepath string) IRoutes { //对静态文件的读取
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("URL parameters can not be used when serving a static file")
	}
	handler := func(c *Context) {
		c.File(filepath)
	}
	group.GET(relativePath, handler)
	group.HEAD(relativePath, handler)
	return group.returnObj()
}

// Static serves files from the given file system root.
// Internally a http.FileServer is used, therefore http.NotFound is used instead
// of the Router's NotFound handler.
// To use the operating system's file system implementation,
// use :
//     router.Static("/static", "/var/www")
func (group *RouterGroup) Static(relativePath, root string) IRoutes { //对静态文件的读取
	return group.StaticFS(relativePath, Dir(root, false))
}

// StaticFS works just like `Static()` but a custom `http.FileSystem` can be used instead.
// Gin by default user: gin.Dir()
func (group *RouterGroup) StaticFS(relativePath string, fs http.FileSystem) IRoutes { //对静态文件的读取
	if strings.Contains(relativePath, ":") || strings.Contains(relativePath, "*") {
		panic("URL parameters can not be used when serving a static folder")
	}
	handler := group.createStaticHandler(relativePath, fs)
	urlPattern := path.Join(relativePath, "/*filepath")

	// Register GET and HEAD handlers
	group.GET(urlPattern, handler)
	group.HEAD(urlPattern, handler)
	return group.returnObj()
}

func (group *RouterGroup) createStaticHandler(relativePath string, fs http.FileSystem) HandlerFunc {
	absolutePath := group.calculateAbsolutePath(relativePath)
	fileServer := http.StripPrefix(absolutePath, http.FileServer(fs))
	_, nolisting := fs.(*onlyfilesFS)
	return func(c *Context) {
		if nolisting {
			c.Writer.WriteHeader(http.StatusNotFound)
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}

func (group *RouterGroup) combineHandlers(handlers HandlersChain) HandlersChain {
	finalSize := len(group.Handlers) + len(handlers)
	if finalSize >= int(abortIndex) {
		panic("too many handlers")
	}
	mergedHandlers := make(HandlersChain, finalSize)
	copy(mergedHandlers, group.Handlers)  //从两个copy的顺序可以看出，group的handler高于自定义的handler。中间件是属于groupHandlers的
	copy(mergedHandlers[len(group.Handlers):], handlers)
	return mergedHandlers
}

func (group *RouterGroup) calculateAbsolutePath(relativePath string) string {
	return joinPaths(group.basePath, relativePath)
}

func (group *RouterGroup) returnObj() IRoutes { //无论是返回engine还是group，他们都属于IRoutes类型，因为他们属于父子，所以只要父类实现了IRoutes中的所有方法，就相当于子类也实现了（即继承）。
	if group.root { //在执行r.Group之前，也就是在进行路由分组之前，此处是true
		return group.engine
	}
	return group  //路由分组后，返回这个。
}
