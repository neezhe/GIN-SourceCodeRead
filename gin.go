// Copyright 2014 Manu Martinez-Almeida.  All rights reserved.
// Use of this source code is governed by a MIT style
// license that can be found in the LICENSE file.

package gin

import (
	"html/template"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin/render"
)

const defaultMultipartMemory = 32 << 20 // 32 MB

var (
	default404Body   = []byte("404 page not found")
	default405Body   = []byte("405 method not allowed")
	defaultAppEngine bool
)

// HandlerFunc defines the handler used by gin middleware as return value.
type HandlerFunc func(*Context)

// HandlersChain defines a HandlerFunc array.
type HandlersChain []HandlerFunc

// Last returns the last handler in the chain. ie. the last handler is the main own.
func (c HandlersChain) Last() HandlerFunc {
	if length := len(c); length > 0 {
		return c[length-1]
	}
	return nil
}

// RouteInfo represents a request route's specification which contains method and path and its handler.
type RouteInfo struct {
	Method  string
	Path    string
	Handler string
}

// RoutesInfo defines a RouteInfo array.
type RoutesInfo []RouteInfo

// Engine is the framework's instance, it contains the muxer, middleware and configuration settings.
// Create an instance of Engine, by using New() or Default()
type Engine struct {//为何不直接把RouterGroup中的方法放到Engine中，这样是因为“路由”和“引擎”毕竟是两个逻辑，使用继承的方式有利于代码逻辑分离。并且gin还定义了接口IRoutes来表示RouterGroup实现的方法。
	RouterGroup  //RouterGroup 描述的是路由的一个父类，里面包含了父节点的一些属性,Engine就继承RouterGroup,为的就是往树中添加节点，这个对象有请求方法的具体实现
//接下来就是几个bool类型的变量，主要是对重定向、转发等一些属性的控制
	// Enables automatic redirection if the current route can't be matched but a
	// handler for the path with (without) the trailing slash exists.
	// For example if /foo/ is requested but a route only exists for /foo, the
	// client is redirected to /foo with http status code 301 for GET requests
	// and 307 for all other request methods.
	RedirectTrailingSlash bool

	// If enabled, the router tries to fix the current request path, if no
	// handle is registered for it.
	// First superfluous path elements like ../ or // are removed.
	// Afterwards the router does a case-insensitive lookup of the cleaned path.
	// If a handle can be found for this route, the router makes a redirection
	// to the corrected path with status code 301 for GET requests and 307 for
	// all other request methods.
	// For example /FOO and /..//Foo could be redirected to /foo.
	// RedirectTrailingSlash is independent of this option.
	RedirectFixedPath bool

	// If enabled, the router checks if another method is allowed for the
	// current route, if the current request can not be routed.
	// If this is the case, the request is answered with 'Method Not Allowed'
	// and HTTP status code 405.
	// If no other Method is allowed, the request is delegated to the NotFound
	// handler.
	HandleMethodNotAllowed bool
	ForwardedByClientIP    bool

	// #726 #755 If enabled, it will thrust some headers starting with
	// 'X-AppEngine...' for better integration with that PaaS.
	AppEngine bool

	// If enabled, the url.RawPath will be used to find parameters.
	UseRawPath bool

	// If true, the path value will be unescaped.
	// If UseRawPath is false (by default), the UnescapePathValues effectively is true,
	// as url.Path gonna be used, which is already unescaped.
	UnescapePathValues bool

	// Value of 'maxMemory' param that is given to http.Request's ParseMultipartForm
	// method call.
	MaxMultipartMemory int64   //从http.Request当中解析处理的最大内存上限

	delims           render.Delims
	secureJsonPrefix string
	HTMLRender       render.HTMLRender
	FuncMap          template.FuncMap
	allNoRoute       HandlersChain
	allNoMethod      HandlersChain
	noRoute          HandlersChain  //存的啥？竟然是小写，那么别的包就无法使用,只能gin包能用
	noMethod         HandlersChain//存的啥？
	pool             sync.Pool  //标准库的，Gin框架里面对于服务器处理请求定义的一个线程池模型，里面包含了线程池的最大上限，以及每个线程的同步异步处理。详情请细读pool源码
	trees            methodTrees  //Redix树结构进行存储路由信息，(基数树) 其实就差不多是传统的二叉树，只是在寻找方式上，利用比如一个unsigned int的类型的每一个比特位作为树节点的判断。
//每种get,post等都有一棵树，每来一个就向相应的树中增加一个节点，
//那么具体往这个trees中增加路由怎么增加呢？这里选择使用一个结构RouterGroup（有各种get/post...的方法）,那么Engine就继承RouterGroup
}

var _ IRouter = &Engine{}

// New returns a new blank Engine instance without any middleware attached.
// By default the configuration is:
// - RedirectTrailingSlash:  true
// - RedirectFixedPath:      false
// - HandleMethodNotAllowed: false
// - ForwardedByClientIP:    true
// - UseRawPath:             false
// - UnescapePathValues:     true
func New() *Engine {  //注意此函数和下面旁边的Default()函数都是用来生成一个Engine的，只不过Default()函数使用了默认的Logger(), Recovery()中间件函数
	debugPrintWARNINGNew()
	engine := &Engine{  //第一步，构造了Engine对象，并传入了所需参数。
		RouterGroup: RouterGroup{
			Handlers: nil,
			basePath: "/",
			root:     true,
		},
		FuncMap:                template.FuncMap{},
		RedirectTrailingSlash:  true,
		RedirectFixedPath:      false,
		HandleMethodNotAllowed: false,
		ForwardedByClientIP:    true,
		AppEngine:              defaultAppEngine,
		UseRawPath:             false,
		UnescapePathValues:     true,
		MaxMultipartMemory:     defaultMultipartMemory,
		trees:                  make(methodTrees, 0, 9),
		delims:                 render.Delims{Left: "{{", Right: "}}"},
		secureJsonPrefix:       "while(1);",
	}
	engine.RouterGroup.engine = engine //第二步，将engine自身的父类指向了自己，因为这里并没有对路由进行分组。
	engine.pool.New = func() interface{} {  //第三步，将pool的New变量指向了一个匿名函数，并返回了包含有engine的Context。
		return engine.allocateContext()  //pool.New指定一个返回对象的方法，主要用于当池里没有临时对象的时候，就用这个方法return一个对象。get put为操作池的方法。
	}
	return engine
}

// Default returns an Engine instance with the Logger and Recovery middleware already attached.
func Default() *Engine {
	debugPrintWARNINGDefault()//就打印两行信息，表示进入
	engine := New()
	engine.Use(Logger(), Recovery())  //这里实际上是传入了默认的中间件，日志和基本异常处理。主要是对请求参数的打印/将异常信息输出到日志中
	return engine
}

func (engine *Engine) allocateContext() *Context {
	return &Context{engine: engine}  //Context里面包含了请求的一系列参数
}

// Delims sets template left and right delims and returns a Engine instance.
func (engine *Engine) Delims(left, right string) *Engine {
	engine.delims = render.Delims{Left: left, Right: right}
	return engine
}

// SecureJsonPrefix sets the secureJsonPrefix used in Context.SecureJSON.
func (engine *Engine) SecureJsonPrefix(prefix string) *Engine {
	engine.secureJsonPrefix = prefix
	return engine
}

// LoadHTMLGlob loads HTML files identified by glob pattern
// and associates the result with HTML renderer.
func (engine *Engine) LoadHTMLGlob(pattern string) {
	left := engine.delims.Left
	right := engine.delims.Right
	templ := template.Must(template.New("").Delims(left, right).Funcs(engine.FuncMap).ParseGlob(pattern))

	if IsDebugging() {
		debugPrintLoadTemplate(templ)
		engine.HTMLRender = render.HTMLDebug{Glob: pattern, FuncMap: engine.FuncMap, Delims: engine.delims}
		return
	}

	engine.SetHTMLTemplate(templ)
}

// LoadHTMLFiles loads a slice of HTML files
// and associates the result with HTML renderer.
func (engine *Engine) LoadHTMLFiles(files ...string) {
	if IsDebugging() {
		engine.HTMLRender = render.HTMLDebug{Files: files, FuncMap: engine.FuncMap, Delims: engine.delims}
		return
	}

	templ := template.Must(template.New("").Delims(engine.delims.Left, engine.delims.Right).Funcs(engine.FuncMap).ParseFiles(files...))
	engine.SetHTMLTemplate(templ)
}

// SetHTMLTemplate associate a template with HTML renderer.
func (engine *Engine) SetHTMLTemplate(templ *template.Template) {
	if len(engine.trees) > 0 {
		debugPrintWARNINGSetHTMLTemplate()
	}

	engine.HTMLRender = render.HTMLProduction{Template: templ.Funcs(engine.FuncMap)}
}

// SetFuncMap sets the FuncMap used for template.FuncMap.
func (engine *Engine) SetFuncMap(funcMap template.FuncMap) {
	engine.FuncMap = funcMap
}

// NoRoute adds handlers for NoRoute. It return a 404 code by default.
func (engine *Engine) NoRoute(handlers ...HandlerFunc) { //显然这个方法是给engine.noRoute赋值的，主要用于用户自定义实现的，比如router.NoRoute(go404)，我们就可以手动实现go404函数。
	engine.noRoute = handlers
	engine.rebuild404Handlers()
}

// NoMethod sets the handlers called when... TODO.
func (engine *Engine) NoMethod(handlers ...HandlerFunc) {
	engine.noMethod = handlers
	engine.rebuild405Handlers()
}

// Use attachs a global middleware to the router. ie. the middleware attached though Use() will be
// included in the handlers chain for every single request. Even 404, 405, static files...
// For example, this is the right place for a logger or error management middleware.
func (engine *Engine) Use(middleware ...HandlerFunc) IRoutes { //可变参数，表示可以添加多个中间件的组件，组建指的是函数句柄，默认只使用了recovery和logger
	engine.RouterGroup.Use(middleware...) //把中间件函数append到engine.RouterGroup.Handlers。noRoute和noMethod是未知路由和未知方法的处理函数，可以像中间件一样自己实现。
	engine.rebuild404Handlers() //把engine.noRoute拷贝到RouterGroup.Handlers，Handlers就成了中间件函数+noRoute函数,显然此处的noRoute和noMethod函数都是空的。
	engine.rebuild405Handlers()//把engine.noMethod拷贝到RouterGroup.Handlers，Handlers就成了中间件函数+noRoute函数+noMethod函数
	//debugPrint("===================", engine.RouterGroup.Handlers)
	return engine
}

func (engine *Engine) rebuild404Handlers() {
	engine.allNoRoute = engine.combineHandlers(engine.noRoute)//engine可以调用其父类RouterGroup中的方法combineHandlers，
}

func (engine *Engine) rebuild405Handlers() {
	engine.allNoMethod = engine.combineHandlers(engine.noMethod)
}

func (engine *Engine) addRoute(method, path string, handlers HandlersChain) {
	assert1(path[0] == '/', "path must begin with '/'")
	assert1(method != "", "HTTP method can not be empty")
	assert1(len(handlers) > 0, "there must be at least one handler")

	debugPrintRoute(method, path, handlers)
	root := engine.trees.get(method) //根据method在树里面找root这棵树，结构体是node
	if root == nil {//如果为空
		root = new(node) //go 自带的new函数，新生成一个node结构体
		engine.trees = append(engine.trees, methodTree{method: method, root: root}) //trees是一个数组，即每一个method都有一个tree
	}
	root.addRoute(path, handlers) //httprouter中构造基数树的核心方法是addRoute, 其公共方法Get, Post只是对addRoute的一个调用
}

// Routes returns a slice of registered routes, including some useful information, such as:
// the http method, path and the handler name.
func (engine *Engine) Routes() (routes RoutesInfo) {
	for _, tree := range engine.trees {
		routes = iterate("", tree.method, routes, tree.root)
	}
	return routes
}

func iterate(path, method string, routes RoutesInfo, root *node) RoutesInfo {
	path += root.path
	if len(root.handlers) > 0 {
		routes = append(routes, RouteInfo{
			Method:  method,
			Path:    path,
			Handler: nameOfFunction(root.handlers.Last()),
		})
	}
	for _, child := range root.children {
		routes = iterate(path, method, routes, child)
	}
	return routes
}

// Run attaches the router to a http.Server and starts listening and serving HTTP requests.
// It is a shortcut for http.ListenAndServe(addr, router)
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) Run(addr ...string) (err error) {//可以接受任意个string参数，可以for来遍历addr
	defer func() { debugPrintError(err) }()

	address := resolveAddress(addr) //1。得到配置的地址以及端口
	debugPrint("Listening and serving HTTP on %s\n", address)
	err = http.ListenAndServe(address, engine) //调http包的接口,2.开启监听模式。因为engine实现了interface中声明的ServeHTTP方法，因此此处可以将engine赋值给interface
	//第2个参数是一个handler类型，这里就是我们的入口，这里我们需要有一个类来实现这个接口：Engine。
	//进入这个函数发现调用http.ListenAndServe之后真正起作用的是Server结构体LisntenAndServe方法，给http.ListenAndServe传递的参数只是用来创建一个Server结构体实例，
	//也就是初始化了Server结构体中的元素Addr/Handler,如果我们不传具体的参数给http.ListenAndServe，那么它会默认以":http"(等价于":80")和DefaulServeMux作为参数来初始化Server结构体。
	//那我们直接看Server.ListenAndServe里面都做了些什么
	return
}

// RunTLS attaches the router to a http.Server and starts listening and serving HTTPS (secure) requests.
// It is a shortcut for http.ListenAndServeTLS(addr, certFile, keyFile, router)
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) RunTLS(addr, certFile, keyFile string) (err error) {
	debugPrint("Listening and serving HTTPS on %s\n", addr)
	defer func() { debugPrintError(err) }()

	err = http.ListenAndServeTLS(addr, certFile, keyFile, engine)
	return
}

// RunUnix attaches the router to a http.Server and starts listening and serving HTTP requests
// through the specified unix socket (ie. a file).
// Note: this method will block the calling goroutine indefinitely unless an error happens.
func (engine *Engine) RunUnix(file string) (err error) {
	debugPrint("Listening and serving HTTP on unix:/%s", file)
	defer func() { debugPrintError(err) }()

	os.Remove(file)
	listener, err := net.Listen("unix", file)
	if err != nil {
		return
	}
	defer listener.Close()
	err = http.Serve(listener, engine)
	return
}
//http包装了内部TCP连接和报文解析的复杂琐碎的细节，使用者只需要和 http.request 和 http.ResponseWriter 两个对象交互就行。
// 也就是说，我们只要写一个 handler（即实现了ServeHTTP的handler），请求会通过参数传递进来，而它要做的就是根据请求的数据做处理，把结果写到 Response 中。
func (engine *Engine) ServeHTTP(w http.ResponseWriter, req *http.Request) { //这里就是我们的入口，第一个参数是interface封装的，这两个传入参数都会存入context
//这里ServeHTTP的方法传递的两个参数，一个是Request，一个是ResponseWriter，Engine中的ServeHTTP的方法就是要对这两个对象进行读取或者写入操作。
//而且这两个对象往往是需要同时存在的，为了避免很多函数都需要写这两个参数，我们不如封装一个结构来把这两个对象放在里面：Context	（相当于一个全局的存在）
// 从上下文对象池中获取一个上下文对象
	c := engine.pool.Get().(*Context) //Context这个上下文对象是在对象池里面取出来的，而不是每次都生成，提高效率，节省对象频繁创建和销毁的代价
	c.writermem.reset(w)// 初始化上下文对象，因为从对象池取出来的数据，有脏数据，故要初始化。将http.ResponseWriter赋值给responseWriter
	c.Request = req
	c.reset()

	engine.handleHTTPRequest(c)//处理web请求，不同的路由有不同的请求处理，那么是怎样调用的？

	engine.pool.Put(c)  //上面处理完后此处将Context对象扔回对象池了
}

// HandleContext re-enter a context that has been rewritten.
// This can be done by setting c.Request.URL.Path to your new target.
// Disclaimer: You can loop yourself to death with this, use wisely.
func (engine *Engine) HandleContext(c *Context) {
	c.reset()
	engine.handleHTTPRequest(c)
	engine.pool.Put(c)
}

func (engine *Engine) handleHTTPRequest(c *Context) { //在请求进来的时候，路由匹配，handleHTTPRequest去Engine中的tree中调用getValue获取出对应的handlers进行处理。
	httpMethod := c.Request.Method //// 获取请求的 Reqeust method 及 path
	path := c.Request.URL.Path
	unescape := false
	if engine.UseRawPath && len(c.Request.URL.RawPath) > 0 {
		path = c.Request.URL.RawPath
		unescape = engine.UnescapePathValues
	}

	// Find root of the tree for the given HTTP method
	t := engine.trees //// tree是个数组，里面保存着对应的请求方式的，URI与处理函数的树。
	// 之所以用数组是因为，在个数少的时候，数组查询比字典要快

	for i, tl := 0, len(t); i < tl; i++ {
		if t[i].method != httpMethod { // // method 匹配
			continue
		}
		root := t[i].root
		// Find route in tree
		handlers, params, tsr := root.getValue(path, c.Params, unescape)// 找到路由对应的处理函数们
//每个请求进来，匹配好路由之后，会获取这个路由最终combine的handlers，把它放在全局的context中（下面的c.handlers），然后通过
// 调用context.Next()来进行递归调用这个handlers（即c.handlers）。当然在中间件里面需要记得调用context.Next() 把控制权还给Context。
		if handlers != nil {  // // handlers 存在，调用处理函数
			c.handlers = handlers
			c.Params = params
			c.Next() //// 从第一个 handler 开始调用
			c.writermem.WriteHeaderNow() // 写 Header
			return
		}
		if httpMethod != "CONNECT" && path != "/" { // // handlers 不存在且 method 不是 CONNECT 且 path 不是 /
			if tsr && engine.RedirectTrailingSlash { // 如果配置是需要尾重定向，执行尾重定向
				redirectTrailingSlash(c)
				return
			}
			if engine.RedirectFixedPath && redirectFixedPath(c, root, engine.RedirectFixedPath) {// 如果不需要尾重定向但是配置了重定向固定 path, 重定向到固定 path
				return
			}
		}
		break
	}

	if engine.HandleMethodNotAllowed { // 如果配置 HandleMethodNotAllowed 为 true 处理 methodNotAllowed
		for _, tree := range engine.trees {
			if tree.method == httpMethod {
				continue
			}
			if handlers, _, _ := tree.root.getValue(path, nil, unescape); handlers != nil {
				c.handlers = engine.allNoMethod
				serveError(c, http.StatusMethodNotAllowed, default405Body)
				return
			}
		}
	}
	c.handlers = engine.allNoRoute
	serveError(c, http.StatusNotFound, default404Body) // 如果找不到匹配 route，返回404
}

var mimePlain = []string{MIMEPlain}

func serveError(c *Context, code int, defaultMessage []byte) {
	c.writermem.status = code
	c.Next()
	if c.writermem.Written() {
		return
	}
	if c.writermem.Status() == code {
		c.writermem.Header()["Content-Type"] = mimePlain
		c.Writer.Write(defaultMessage)
		return
	}
	c.writermem.WriteHeaderNow()
	return
}

func redirectTrailingSlash(c *Context) {
	req := c.Request
	path := req.URL.Path
	code := http.StatusMovedPermanently // Permanent redirect, request with GET method
	if req.Method != "GET" {
		code = http.StatusTemporaryRedirect
	}

	req.URL.Path = path + "/"
	if length := len(path); length > 1 && path[length-1] == '/' {
		req.URL.Path = path[:length-1]
	}
	debugPrint("redirecting request %d: %s --> %s", code, path, req.URL.String())
	http.Redirect(c.Writer, req, req.URL.String(), code)
	c.writermem.WriteHeaderNow()
}

func redirectFixedPath(c *Context, root *node, trailingSlash bool) bool {
	req := c.Request
	path := req.URL.Path

	if fixedPath, ok := root.findCaseInsensitivePath(cleanPath(path), trailingSlash); ok {
		code := http.StatusMovedPermanently // Permanent redirect, request with GET method
		if req.Method != "GET" {
			code = http.StatusTemporaryRedirect
		}
		req.URL.Path = string(fixedPath)
		debugPrint("redirecting request %d: %s --> %s", code, path, req.URL.String())
		http.Redirect(c.Writer, req, req.URL.String(), code)
		c.writermem.WriteHeaderNow()
		return true
	}
	return false
}
