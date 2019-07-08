// Copyright 2013 Julien Schmidt. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be found
// at https://github.com/julienschmidt/httprouter/blob/master/LICENSE

package gin

import (
	"net/url"
	"strings"
	"unicode"
)

// Param is a single URL parameter, consisting of a key and a value.
type Param struct {
	Key   string
	Value string
}

// Params is a Param-slice, as returned by the router.
// The slice is ordered, the first URL parameter is also the first slice value.
// It is therefore safe to read values by the index.
type Params []Param

// Get returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) Get(name string) (string, bool) {
	for _, entry := range ps {
		if entry.Key == name {
			return entry.Value, true
		}
	}
	return "", false
}

// ByName returns the value of the first Param which key matches the given name.
// If no matching Param is found, an empty string is returned.
func (ps Params) ByName(name string) (va string) {
	va, _ = ps.Get(name)
	return
}

type methodTree struct {
	method string
	root   *node
}

type methodTrees []methodTree

func (trees methodTrees) get(method string) *node {
	for _, tree := range trees {
		if tree.method == method {
			return tree.root
		}
	}
	return nil
}

func min(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func countParams(path string) uint8 {
	var n uint
	for i := 0; i < len(path); i++ {
		if path[i] != ':' && path[i] != '*' {
			continue
		}
		n++
	}
	if n >= 255 {
		return 255
	}
	return uint8(n)
}

type nodeType uint8

const (
	static nodeType = iota // default，静态节点，进行普通匹配
	root
	param
	catchAll
)
//httprouter为了提高扩展性和性能，通过priority值对整个树的节点进行优先级排序，
// 保证被最多路径包含的节点最先进行匹配，最长路径的节点最先被评估（从上到下，从左到右的顺序）。


//通过基数树可以提高字符顺序匹配的效率，对于URL之类的字符使用基数树来进行归类、匹配非常适合
type node struct { //基数树节点的定义
	path      string // 每个节点的匹配路径，  // 节点路径，比如上面的s，earch，和upport
	indices   string
	// indices保存节点与子节点的分裂的第一个字符
	// 如aboutme，与aboutteam两个路径，同属于about节点，但是后续会跟随me， team两个子节点
	// 此时indices就是"mt"
	children  []*node //// 子节点
	handlers  HandlersChain  // http请求处理方法，算上中间件的函数就有多个方法。
	priority  uint32// 节点权重（子节点的handler总数），这是httprouter为提高查找性能做的优化
	nType     nodeType
	// 节点类型nType
	// static: 静态节点，进行普通匹配
	// root： 根节点
	// param： 参数节点(:)
	// catchAll：以*匹配的接口
	maxParams uint8 // 节点路径的最大参数个数
	wildChild bool //是否为参数节点(path中包含*,:)
}

// increments priority of the given child and reorders if necessary.
func (n *node) incrementChildPrio(pos int) int {
	n.children[pos].priority++
	prio := n.children[pos].priority

	// adjust position (move to front)
	newPos := pos
	for newPos > 0 && n.children[newPos-1].priority < prio {
		// swap node positions
		n.children[newPos-1], n.children[newPos] = n.children[newPos], n.children[newPos-1]

		newPos--
	}

	// build new index char string
	if newPos != pos {
		n.indices = n.indices[:newPos] + // unchanged prefix, might be empty
			n.indices[pos:pos+1] + // the index char we move
			n.indices[newPos:pos] + n.indices[pos+1:] // rest without char at 'pos'
	}

	return newPos
}

// addRoute adds a node with the given handle to the path.
// Not concurrency-safe!
func (n *node) addRoute(path string, handlers HandlersChain) {
	fullPath := path
	n.priority++
	numParams := countParams(path)

	// 如果是非空树
	if len(n.path) > 0 || len(n.children) > 0 {
	walk:
		for {
			// Update maxParams of the current node
			if numParams > n.maxParams {
				n.maxParams = numParams
			}

			// 查找当前节点路径与将要匹配路径的最长公共前缀，并记录下索引位置
			i := 0
			max := min(len(path), len(n.path))
			for i < max && path[i] == n.path[i] {
				i++
			}
			// 开始分裂，比如一开始path是search，新来了support，s是他们匹配的部分，
			// 那么会将s拿出来作为父节点，增加earch和upport作为child节点
			// 如果当前节点的路径不是新加入路径的最长公共前缀
			// 从该节点分裂出一个子节点
			if i < len(n.path) {
				child := node{
					path:      n.path[i:],  //// 不匹配的部分作为child节点
					wildChild: n.wildChild,
					indices:   n.indices,
					children:  n.children,
					handlers:  n.handlers,
					priority:  n.priority - 1, //// 降级成子节点，priority减1
				}

				// Update maxParams (max of all children)
				for i := range child.children {
					if child.children[i].maxParams > child.maxParams {
						child.maxParams = child.children[i].maxParams
					}
				}
				// 当前节点的子节点变成刚刚分裂的出来的节点
				n.children = []*node{&child}
				// []byte for proper unicode char conversion, see #65
				n.indices = string([]byte{n.path[i]})
				n.path = path[:i]
				n.handlers = nil
				n.wildChild = false
			}

			// 将新路径作为子节点插入当前节点
			if i < len(path) {
				path = path[i:]// 记录最大前缀匹配位置后的字符串，以便继续向下匹配
				// 公共节点是一个参数节点，说明在前面的最长前缀匹配时两个路径类似:path, :path/rest/path
				// 这是继续需要进行从子节点匹配
				if n.wildChild {
					n = n.children[0]
					n.priority++

					// Update maxParams of the child node
					if numParams > n.maxParams {
						n.maxParams = numParams
					}
					numParams--

					// Check if the wildcard matches
					// 判断子节点（n）与后续路径是否存在匹配， 例如：/blog/:pp 和 /blog/:ppp，需要检查更长的通配符, 否则表示路径存在冲突
					if len(path) >= len(n.path) && n.path == path[:len(n.path)] {
						// check for longer wildcard, e.g. :name and :names
						if len(n.path) >= len(path) || path[len(n.path)] == '/' {
							continue walk
						}
					}

					panic("path segment '" + path +
						"' conflicts with existing wildcard '" + n.path +
						"' in path '" + fullPath + "'")
				}

				c := path[0]// 首字母，用来与indices做比较

				// slash after param
				if n.nType == param && c == '/' && len(n.children) == 1 {
					n = n.children[0]
					n.priority++
					continue walk
				}
				// 通过indices与后续路径的首字母比较，可以知道后续应该从哪个子节点进行匹配插入
				// 比如s的子节点现在是earch和upport，indices为eu
				// 如果新来的路由为super，那么就是和upport有匹配的部分u，将继续分类现在的upport节点
				// Check if a child with the next path byte exists
				for i := 0; i < len(n.indices); i++ {
					if c == n.indices[i] {
						i = n.incrementChildPrio(i)
						n = n.children[i]
						continue walk
					}
				}

				// Otherwise insert it
				if c != ':' && c != '*' {
					// []byte for proper unicode char conversion, see #65
					n.indices += string([]byte{c})  // 记录第一个字符，放在indices中
					child := &node{
						maxParams: numParams,
					}// 增加子节点
					n.children = append(n.children, child)
					n.incrementChildPrio(len(n.indices) - 1)
					n = child
				}
				n.insertChild(numParams, path, fullPath, handlers)
				return

			} else if i == len(path) { // Make node a (in-path) leaf
				if n.handlers != nil {    // 路径相同，如果已有handler就报错，没有就赋值
					panic("handlers are already registered for path '" + fullPath + "'")
				}
				n.handlers = handlers
			}
			return
		}
	} else { // 空树的话 添加根节点
		n.insertChild(numParams, path, fullPath, handlers)
		n.nType = root
	}
}
//上面函数的主要目的是找到插入节点的位置，下面是插入函数
//sertChild函数是根据path本身进行分割, 将/分开的部分分别作为节点保存, 形成一棵树结构.
// 注意参数匹配中的:和*的区别, 前者是匹配一个字段, 后者是匹配后面所有的路径
func (n *node) insertChild(numParams uint8, path string, fullPath string, handlers HandlersChain) {
	var offset int // already handled bytes of the path

	// find prefix until first wildcard (beginning with ':' or '*')
	for i, max := 0, len(path); numParams > 0; i++ { // 找到前缀，只要匹配到wildcard
		c := path[i]
		if c != ':' && c != '*' {
			continue
		}

		// find wildcard end (either '/' or path end)
		end := i + 1
		for end < max && path[end] != '/' {
			switch path[end] {
			// the wildcard name must not contain ':' and '*'
			case ':', '*':
				panic("only one wildcard per path segment is allowed, has: '" +
					path[i:] + "' in path '" + fullPath + "'")
			default:
				end++
			}
		}

		// check if this Node existing children which would be
		// unreachable if we insert the wildcard here
		if len(n.children) > 0 {
			panic("wildcard route '" + path[i:end] +
				"' conflicts with existing children in path '" + fullPath + "'")
		}

		// check if the wildcard has a name
		if end-i < 2 {
			panic("wildcards must be named with a non-empty name in path '" + fullPath + "'")
		}

		if c == ':' { // param
			// split path at the beginning of the wildcard
			if i > 0 {
				n.path = path[offset:i]
				offset = i
			}

			child := &node{
				nType:     param,
				maxParams: numParams,
			}
			n.children = []*node{child}
			n.wildChild = true
			n = child
			n.priority++
			numParams--

			// if the path doesn't end with the wildcard, then there
			// will be another non-wildcard subpath starting with '/'
			if end < max {
				n.path = path[offset:end]
				offset = end

				child := &node{
					maxParams: numParams,
					priority:  1,
				}
				n.children = []*node{child}
				n = child  // 下次循环这个新的child节点
			}

		} else { // catchAll
			if end != max || numParams > 1 {
				panic("catch-all routes are only allowed at the end of the path in path '" + fullPath + "'")
			}

			if len(n.path) > 0 && n.path[len(n.path)-1] == '/' {
				panic("catch-all conflicts with existing handle for the path segment root in path '" + fullPath + "'")
			}

			// currently fixed width 1 for '/'
			i--
			if path[i] != '/' {
				panic("no / before catch-all in path '" + fullPath + "'")
			}

			n.path = path[offset:i]

			// first node: catchAll node with empty path
			child := &node{
				wildChild: true,
				nType:     catchAll,
				maxParams: 1,
			}
			n.children = []*node{child}
			n.indices = string(path[i])
			n = child
			n.priority++

			// second node: node holding the variable
			child = &node{
				path:      path[i:],
				nType:     catchAll,
				maxParams: 1,
				handlers:  handlers,
				priority:  1,
			}
			n.children = []*node{child}

			return
		}
	}

	// insert remaining path part and handle to the leaf
	n.path = path[offset:]
	n.handlers = handlers
}

// getValue returns the handle registered with the given path (key). The values of
// wildcards are saved to a map.
// If no handle can be found, a TSR (trailing slash redirect) recommendation is
// made if a handle exists with an extra (without the) trailing slash for the
// given path.
func (n *node) getValue(path string, po Params, unescape bool) (handlers HandlersChain, p Params, tsr bool) {//路由的匹配
	p = po
walk: // Outer loop for walking the tree
	for {
		if len(path) > len(n.path) {  // 尚未到达path的终点，
			// 前缀匹配
			if path[:len(n.path)] == n.path {// // 前面一段需要一致
				path = path[len(n.path):]
				// If this node does not have a wildcard (param or catchAll)
				// child,  we can just look up the next child node and continue
				// to walk down the tree
				if !n.wildChild { // 找到下一步要匹配的子节点
					c := path[0]
					for i := 0; i < len(n.indices); i++ {
						if c == n.indices[i] {
							n = n.children[i]
							continue walk
						}
					}

					// Nothing found.
					// We can recommend to redirect to the same URL without a
					// trailing slash if a leaf exists for that path.
					tsr = path == "/" && n.handlers != nil
					return
				}

				// handle wildcard child
				n = n.children[0]
				switch n.nType {
				case param:
					// find param end (either '/' or path end)
					end := 0
					for end < len(path) && path[end] != '/' {
						end++
					}

					// save param value
					if cap(p) < int(n.maxParams) {
						p = make(Params, 0, n.maxParams)
					}
					i := len(p)
					p = p[:i+1] // expand slice within preallocated capacity
					p[i].Key = n.path[1:]
					val := path[:end]
					if unescape {
						var err error
						if p[i].Value, err = url.QueryUnescape(val); err != nil {
							p[i].Value = val // fallback, in case of error
						}
					} else {
						p[i].Value = val
					}

					// we need to go deeper!
					if end < len(path) {
						if len(n.children) > 0 {
							path = path[end:]
							n = n.children[0]
							continue walk
						}

						// ... but we can't
						tsr = len(path) == end+1
						return
					}

					if handlers = n.handlers; handlers != nil {
						return
					}
					if len(n.children) == 1 {
						// No handle found. Check if a handle for this path + a
						// trailing slash exists for TSR recommendation
						n = n.children[0]
						tsr = n.path == "/" && n.handlers != nil
					}

					return

				case catchAll:
					// save param value
					if cap(p) < int(n.maxParams) {
						p = make(Params, 0, n.maxParams)
					}
					i := len(p)
					p = p[:i+1] // expand slice within preallocated capacity
					p[i].Key = n.path[2:]
					if unescape {
						var err error
						if p[i].Value, err = url.QueryUnescape(path); err != nil {
							p[i].Value = path // fallback, in case of error
						}
					} else {
						p[i].Value = path
					}

					handlers = n.handlers
					return

				default:
					panic("invalid node type")
				}
			}
		} else if path == n.path {
			// We should have reached the node containing the handle.
			// Check if this node has a handle registered.
			if handlers = n.handlers; handlers != nil {
				return
			}

			if path == "/" && n.wildChild && n.nType != root {
				tsr = true
				return
			}

			// No handle found. Check if a handle for this path + a
			// trailing slash exists for trailing slash recommendation
			for i := 0; i < len(n.indices); i++ {
				if n.indices[i] == '/' {
					n = n.children[i]
					tsr = (len(n.path) == 1 && n.handlers != nil) ||
						(n.nType == catchAll && n.children[0].handlers != nil)
					return
				}
			}

			return
		}

		// Nothing found. We can recommend to redirect to the same URL with an
		// extra trailing slash if a leaf exists for that path
		tsr = (path == "/") ||
			(len(n.path) == len(path)+1 && n.path[len(path)] == '/' &&
				path == n.path[:len(n.path)-1] && n.handlers != nil)
		return
	}
}

// findCaseInsensitivePath makes a case-insensitive lookup of the given path and tries to find a handler.
// It can optionally also fix trailing slashes.
// It returns the case-corrected path and a bool indicating whether the lookup
// was successful.
func (n *node) findCaseInsensitivePath(path string, fixTrailingSlash bool) (ciPath []byte, found bool) {
	ciPath = make([]byte, 0, len(path)+1) // preallocate enough memory

	// Outer loop for walking the tree
	for len(path) >= len(n.path) && strings.ToLower(path[:len(n.path)]) == strings.ToLower(n.path) {
		path = path[len(n.path):]
		ciPath = append(ciPath, n.path...)

		if len(path) > 0 {
			// If this node does not have a wildcard (param or catchAll) child,
			// we can just look up the next child node and continue to walk down
			// the tree
			if !n.wildChild {
				r := unicode.ToLower(rune(path[0]))
				for i, index := range n.indices {
					// must use recursive approach since both index and
					// ToLower(index) could exist. We must check both.
					if r == unicode.ToLower(index) {
						out, found := n.children[i].findCaseInsensitivePath(path, fixTrailingSlash)
						if found {
							return append(ciPath, out...), true
						}
					}
				}

				// Nothing found. We can recommend to redirect to the same URL
				// without a trailing slash if a leaf exists for that path
				found = fixTrailingSlash && path == "/" && n.handlers != nil
				return
			}

			n = n.children[0]
			switch n.nType {
			case param:
				// find param end (either '/' or path end)
				k := 0
				for k < len(path) && path[k] != '/' {
					k++
				}

				// add param value to case insensitive path
				ciPath = append(ciPath, path[:k]...)

				// we need to go deeper!
				if k < len(path) {
					if len(n.children) > 0 {
						path = path[k:]
						n = n.children[0]
						continue
					}

					// ... but we can't
					if fixTrailingSlash && len(path) == k+1 {
						return ciPath, true
					}
					return
				}

				if n.handlers != nil {
					return ciPath, true
				} else if fixTrailingSlash && len(n.children) == 1 {
					// No handle found. Check if a handle for this path + a
					// trailing slash exists
					n = n.children[0]
					if n.path == "/" && n.handlers != nil {
						return append(ciPath, '/'), true
					}
				}
				return

			case catchAll:
				return append(ciPath, path...), true

			default:
				panic("invalid node type")
			}
		} else {
			// We should have reached the node containing the handle.
			// Check if this node has a handle registered.
			if n.handlers != nil {
				return ciPath, true
			}

			// No handle found.
			// Try to fix the path by adding a trailing slash
			if fixTrailingSlash {
				for i := 0; i < len(n.indices); i++ {
					if n.indices[i] == '/' {
						n = n.children[i]
						if (len(n.path) == 1 && n.handlers != nil) ||
							(n.nType == catchAll && n.children[0].handlers != nil) {
							return append(ciPath, '/'), true
						}
						return
					}
				}
			}
			return
		}
	}

	// Nothing found.
	// Try to fix the path by adding / removing a trailing slash
	if fixTrailingSlash {
		if path == "/" {
			return ciPath, true
		}
		if len(path)+1 == len(n.path) && n.path[len(path)] == '/' &&
			strings.ToLower(path) == strings.ToLower(n.path[:len(path)]) &&
			n.handlers != nil {
			return append(ciPath, n.path...), true
		}
	}
	return
}
