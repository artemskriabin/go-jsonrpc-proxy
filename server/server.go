package server

import (
	"bytes"
	"fmt"
	"github.com/artemskriabin/go-jsonrpc-proxy/config"
	"github.com/patrickmn/go-cache"
	"github.com/sb-im/jsonrpc-lite"
	"gopkg.in/square/go-jose.v2/json"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"time"
)

var methodNameCache *cache.Cache

var methodPriorityListOrder []MethodRegExp

// MethodRegExp type representing a JSON-RPC method with a compiled
// RegExp entity
type MethodRegExp struct {
	Name       string
	NameRegexp regexp.Regexp
	ProxyTo    []string
}

// NewMethodRegExp returns a new MethodRegExp instance
func NewMethodRegExp(name string, nameRegexp regexp.Regexp, proxyTo []string) MethodRegExp {
	return MethodRegExp{
		Name:       name,
		NameRegexp: nameRegexp,
		ProxyTo:    proxyTo,
	}
}

// LoadMap loads the configuration to an array of MethodRegExp
func LoadMap(config config.Configuration) {
	methodNameCache = cache.New(5*time.Minute, 5*time.Minute)
	methodPriorityListOrder = []MethodRegExp{}
	for _, method := range config.Methods {
		compiledMethodName, errCompile := regexp.Compile(method.Name)
		if errCompile != nil {
			log.Panicf("Config file contains an invalid regex in method's name: %v", method.Name)
		}
		methodRegExpObj := NewMethodRegExp(method.Name, *compiledMethodName, method.ProxyTo)
		methodPriorityListOrder = append(methodPriorityListOrder, methodRegExpObj)
	}
}

// Serve a reverse proxy for a given url
func serveReverseProxy(target string, res http.ResponseWriter, req *http.Request) {
	// parse the url
	url, _ := url.Parse(target)

	// create the reverse proxy
	proxy := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(url)
			r.Out.Host = url.Host // if desired
			r.Out.URL.Path = url.Path
		},
	}

	// Update the headers to allow for SSL redirection
	req.URL.Host = url.Host
	req.URL.Scheme = url.Scheme
	req.Header.Set("X-Forwarded-Host", req.Header.Get("Host"))
	req.Host = url.Host

	// The ServeHttp is non-blocking and uses a go routine under the hood
	proxy.ServeHTTP(res, req)
}

func requestBody(request *http.Request) *bytes.Buffer {
	// Read body to buffer
	body, err := ioutil.ReadAll(request.Body)
	if err != nil {
		log.Printf("Error reading body: %v", err)
		panic(err)
	}

	request.Body = ioutil.NopCloser(bytes.NewBuffer(body))

	return bytes.NewBuffer(body)
}

func parseRequestBody(request *http.Request) (*jsonrpc.Jsonrpc, error) {
	buffered := requestBody(request)
	req, err := jsonrpc.Parse(buffered.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse body, %w", err)
	}
	if req == nil {
		log.Printf("could not parse the JSON-RPC")
	}
	return req, nil
}

// HandleRequestAndRedirect given a request send it to the appropriate url
func HandleRequestAndRedirect(res http.ResponseWriter, req *http.Request) {
	requestPayload, err := parseRequestBody(req)
	if err != nil {
		rpcErr := &jsonrpc.Errors{}
		rpcErr.ParseError(err)
		internalErrorBytes, _ := json.Marshal(rpcErr)
		res.Write(internalErrorBytes)
		return
	}

	url, errRedir := getRedirectTo(requestPayload)

	if errRedir != nil {
		errObjBytes, errJSONMarshal := json.Marshal(errRedir)
		if errJSONMarshal != nil {
			internalError := &jsonrpc.Errors{}
			internalError.InternalError(errJSONMarshal)
			internalErrorBytes, _ := json.Marshal(internalError)
			res.Write(internalErrorBytes)
			return
		}
		res.Write(errObjBytes)
		return
	}

	serveReverseProxy(*url, res, req)
}

// HandlerWrapper type for the ServeHTTP function
type HandlerWrapper func(w http.ResponseWriter, r *http.Request)

func (h *HandlerWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	HandleRequestAndRedirect(w, r)
}

func getRedirectTo(req *jsonrpc.Jsonrpc) (*string, *jsonrpc.Errors) {
	if methodNameCache == nil {
		err := jsonrpc.Errors{}
		err.InternalError("cache not loaded")
		return nil, &err
	}
	if value, ok := methodNameCache.Get(req.Method); ok {
		methodRegEx := value.(MethodRegExp)
		randomRedirectTo := getRandomElem(methodRegEx.ProxyTo)
		return &randomRedirectTo, nil
	}
	for _, method := range methodPriorityListOrder {
		if method.NameRegexp.MatchString(req.Method) {
			methodNameCache.SetDefault(req.Method, method)
			randomRedirectTo := getRandomElem(method.ProxyTo)
			return &randomRedirectTo, nil
		}
	}

	err := jsonrpc.Errors{}
	err.MethodNotFound(req.Method)
	return nil, &err
}

func getRandomElem(array []string) string {
	return array[rand.Intn(len(array))]
}
