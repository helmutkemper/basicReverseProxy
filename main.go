package main

import (
  "net/http"
  "net/url"
  "fmt"
  "regexp"
  "math/rand"
  "time"
  "log"
  "bytes"
  "strconv"
  "io/ioutil"
  "net"
  "sync"
  "strings"
  "context"
  "io"
  "encoding/json"
)

const ListeningPort = "8888"
//const sessionName = "SESSION_ID"
const sessionUniqueIdLength = 30
const attemptsBeforeError = 3

var sessionUniqueIdBase []string
func init() {
  sessionUniqueIdBase = []string{ "a" ,"b" ,"c" ,"d" ,"e" ,"f" ,"g" ,"h" ,"i" ,"j" ,"k" ,"l" ,"m" ,"n" ,"o" ,"p" ,"q" ,"r" ,"s" ,"t" ,"u" ,"v" ,"w" ,"x" ,"y" ,"z" ,"A" ,"B" ,"C" ,"D" ,"E" ,"F" ,"G" ,"H" ,"I" ,"J" ,"K" ,"L" ,"M" ,"N" ,"O" ,"P" ,"Q" ,"R" ,"S" ,"T" ,"U" ,"V" ,"W" ,"X" ,"Y" ,"Z" ,"0" ,"1" ,"2" ,"3" ,"4" ,"5" ,"6" ,"7" ,"8" ,"9" ,"0" ,"_", }
}

func sessionId() string {
  ret := ""
  for i := 0; i != sessionUniqueIdLength; i += 1 {
    ret += sessionUniqueIdBase[ rand.Int() % len( sessionUniqueIdBase ) ]
  }

  return ret
}

var mainRoute config

func main() {
  mainRoute = config{
    ConsecutiveErrorsToDisable: 3,
    TimeToKeepDisabled: time.Second * 30,
    TimeToVerifyDisabled: time.Second * 10,
    Routes: []route{
      {
        Name: "blog",
        Domain: domain{
          SubDomain: "blog",
          Domain: "localhost",
          Port: "8888",
          ExpReg: false,
        },
        Path: path{
          Path: "",
          ExpReg: false,
        },
        ProxyEnable: true,
        ProxyServers: []proxyUrl{
/*          {
            Name: "docker 1 - error",
            Url: "http://localhost:2367",
          },
          {
            Name: "docker 2 - error",
            Url: "http://localhost:2367",
          },*/
          {
            Name: "docker 3 - ok",
            Url: "http://localhost:2368",
          },
        },
      },
      {
        Name: "hello",
        Domain: domain{
          SubDomain: "",
          Domain: "localhost",
          Port: "8888",
          ExpReg: false,
        },
        Path: path{
          Path: "/hello/",
          ExpReg: false,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: hello,
        },
      },
      {
        Name: "panel",
        Domain: domain{
          SubDomain: "root",
          Domain: "localhost",
          Port: "8888",
          ExpReg: false,
        },
        Path: path{
          Path: "/statistic",
          ExpReg: false,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: statistic,
        },
      },
    },
  }
  mainRoute.Prepare()
  go mainRoute.VerifyDisabled()

  // all other traffic pass on
  http.HandleFunc("/", ProxyFunc)
  http.ListenAndServe(":"+ListeningPort, nil)
}

type config struct {
  /*
  Quantidades de erros consecutivos para desabilitar uma rota do proxy.
  A ideia é que uma rota do proxy possa está dando erro temporário, assim, o código desabilita a rota por um tempo e
  depois habilita de novo para testar se a mesma voltou.
  Caso haja apenas uma instabilidade, a rota continua.
  */
  ConsecutiveErrorsToDisable      int64

  /*
  Tempo para manter uma rota do proxy desabilitada antes de testar novamente
  */
  TimeToKeepDisabled              time.Duration

  /*
  Há uma função em loop infinito e a cada x período de tempo, ela verifica se alguma rota está desabilitada e reabilita
  caso o tempo de espera tenha sido excedido
  */
  TimeToVerifyDisabled            time.Duration

  /*
  Rotas do servidor proxy
  */
  Routes                          []route
}

// Inicializa algumas variáveis
func(el *config)Prepare(){
  for routesKey := range el.Routes{
    for urlKey := range el.Routes[ routesKey ].ProxyServers {
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].Busy = false
      el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled = true
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].UsedSuccessfully = 0
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].ErrorCounter = 0
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].ErrorConsecutiveCounter = 0
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].LastLoopError = false
      //el.Routes[ routesKey ].ProxyServers[ urlKey ].TotalTime = time.Second * 0
    }
  }
}

// Verifica se há urls do proxy desabilitadas e as habilita depois de um tempo
// A ideia é que o servidor possa está fora do ar por um tempo, por isto, ele remove a rota por algum tempo, para evitar
// chamadas desnecessárias ao servidor
func(el *config)VerifyDisabled(){
  for {
    for routesKey := range el.Routes {
      for urlKey := range el.Routes[ routesKey ].ProxyServers {
        if time.Since(el.Routes[ routesKey ].ProxyServers[ urlKey ].DisabledSince) >= el.TimeToKeepDisabled && el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled == false {
          el.Routes[ routesKey ].ProxyServers[ urlKey ].ErrorConsecutiveCounter = 0
          el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled = true
        }
      }
    }

    time.Sleep(el.TimeToVerifyDisabled)
  }
}

type proxyUrl struct {
  /*
  Url da rota para o proxy
  */
  Url                     string                `json:"url"`

  /*
  Nome da rota para manter organizado
  */
  Name                    string                `json:"name"`

  /*
  Tempo total de execução da rota.
  A soma de todos os tempos de resposta
  */
  TotalTime               time.Duration         `json:"totalTime"`

  /*
  Quantidades de usos sem erro
  */
  UsedSuccessfully        int64                 `json:"usedSuccessfully"`

  /*
  Habilitada / Desabilitada temporariamente para esperar a rota voltar a responder
  */
  Enabled                 bool                  `json:"enabled"`

  /*
  Total de erros durante a execução da rota do proxy
  */
  ErrorCounter            int64                 `json:"errorCounter"`

  /*
  Conta quantos erros seguidos houveram para poder decidir se desabilita a roda do proxy
  */
  ErrorConsecutiveCounter int64                 `json:"errorConsecutiveCounter"`

  /*
  Indica se a rota está ocupada.
  A ideia é usar em containers na estrutura do servidor
  */
  Busy                    bool                  `json:"busy"`

  /*
  Arquiva o tempo desabilitado para poder reabilitar por time out
  */
  DisabledSince           time.Time             `json:"-"`

  /*
  Usado pelo código para evitar que uma rota fique em loop infinito
  */
  LastLoopError           bool                  `json:"lastLoopError"`
}

type route struct {
  /*
  Nome para o log e outras funções, deve ser único e começar com letra ou '_'
  */
  Name                  string

  /*
  Dados do domínio
  */
  Domain                domain

  /*
  [opcional] Dados do caminho dentro do domínio
  */
  Path                  path

  /*
  [opcional] Dado da aplicação local
  */
  Handle                handle

  /*
  Habilita a funcionalidade do proxy, caso contrário, será chamada a função handle
  */
  ProxyEnable           bool

  /*
  Lista de todas as URLs para os containers com a aplicação
  */
  ProxyServers          []proxyUrl

  /*
  [uso interno] Contador de próximo container
  */
  //balanceCounter        int64
}
type domain struct {
  /*
  [opcional] sub domínio sem ponto final. Ex.: blog.domínio.com fica apenas blog
  */
  SubDomain             string

  /*
  Domínio onde o sistema roda. Foi imaginado para ser textual, por isto, evite ip address
  */
  Domain                string

  /*
  [opcional] Coloque apenas o número da porta, sem os ':'. Ex. :8080, fica apenas 8080
  */
  Port                  string

  ExpReg                bool
}
type path struct {
  /*
  [opcional] Quando omitido, faz com que todo o subdomínio seja usado para a rota
  */
  Path                  string
  ExpReg                bool
}
type handle struct {
  Handle                http.HandlerFunc
}

func hello(w http.ResponseWriter, r *http.Request) {
  w.Write( []byte("Hello") )
}

func statistic(w http.ResponseWriter, r *http.Request) {
  type dataOutProxySerer struct{
    Url                     string                `json:"url"`
    Name                    string                `json:"name"`
    AverageTime             int                   `json:"averageTime"`
    UsedSuccessfully        int64                 `json:"usedSuccessfully"`
    Enabled                 bool                  `json:"enabled"`
    ErrorCounter            int64                 `json:"errorCounter"`
  }
  type dataOut struct{
    Name              string
    ProxyServers      []dataOutProxySerer
    Domain            domain
    Path              path
  }

  var toJson = make( []dataOut, len( mainRoute.Routes ) )

  for key, value := range mainRoute.Routes {

    toJson[ key ] = dataOut{
      Name: value.Name,
      Domain: value.Domain,
      Path: value.Path,
    }

    toJson[ key ].ProxyServers = make( []dataOutProxySerer, len( mainRoute.Routes[ key ].ProxyServers ) )

    for proxyKey := range mainRoute.Routes[ key ].ProxyServers {
      toJson[ key ].ProxyServers[ proxyKey ].Url = mainRoute.Routes[ key ].ProxyServers[ proxyKey ].Url
      toJson[ key ].ProxyServers[ proxyKey ].Name = mainRoute.Routes[ key ].ProxyServers[ proxyKey ].Name
      toJson[ key ].ProxyServers[ proxyKey ].UsedSuccessfully = mainRoute.Routes[ key ].ProxyServers[ proxyKey ].UsedSuccessfully
      toJson[ key ].ProxyServers[ proxyKey ].Enabled = mainRoute.Routes[ key ].ProxyServers[ proxyKey ].Enabled
      toJson[ key ].ProxyServers[ proxyKey ].ErrorCounter = mainRoute.Routes[ key ].ProxyServers[ proxyKey ].ErrorCounter
      if mainRoute.Routes[ key ].ProxyServers[ proxyKey ].UsedSuccessfully == 0 {
        toJson[ key ].ProxyServers[ proxyKey ].AverageTime = 0
      } else {
        toJson[ key ].ProxyServers[ proxyKey ].AverageTime = int( mainRoute.Routes[ key ].ProxyServers[ proxyKey ].TotalTime ) / int(mainRoute.Routes[ key ].ProxyServers[ proxyKey ].UsedSuccessfully) / 1000000
      }
    }


  }

  out, err := json.Marshal( toJson )

  if err != nil {
    w.Write( []byte( err.Error() ) )
    return
  }
  w.Write( out )

}

func ProxyFunc(w http.ResponseWriter, r *http.Request) {

  now := time.Now()

  start := time.Now()
  var handleName string
  defer timeMensure( start, handleName )

  for keyRoute, route := range mainRoute.Routes {

    handleName = route.Name

    if route.Domain.SubDomain != "" {
      route.Domain.SubDomain += "."
    }

    if route.Domain.Port != "" {
      route.Domain.Port = ":" + route.Domain.Port
    }

    if route.Domain.ExpReg == true && route.Path.ExpReg == true {

    } else if route.Domain.ExpReg == false && route.Path.ExpReg == true {

    } else if route.Domain.ExpReg == true && route.Path.ExpReg == false {

    } else if route.Domain.ExpReg == false && route.Path.ExpReg == false {

      if r.Host == route.Domain.SubDomain + route.Domain.Domain + route.Domain.Port && ( route.Path.Path == r.URL.Path || route.Path.Path == "" ) {

        if route.ProxyEnable == false {
          route.Handle.Handle(w, r)

          return
        }

        loopCounter := 0

        for {


          passBusy := false
          passEnabled := false
          keyUrlToUse := 0
          externalServerUrl := ""
          externalServerName := ""
          // procura por containers desocupados
          for urlKey := range mainRoute.Routes[ keyRoute ].ProxyServers{
            if mainRoute.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopError == true {
              continue
            }

            if mainRoute.Routes[ keyRoute ].ProxyServers[ urlKey ].Busy == false {
              passBusy = true
            }

            if mainRoute.Routes[ keyRoute ].ProxyServers[ urlKey ].Enabled == true {
              passEnabled = true
            }

            if passBusy == true && passEnabled == true {
              keyUrlToUse = urlKey
              break
            }
          }

          if passBusy == false && passEnabled == false {        // todos os servidores externos estão ocupados ou desabilitados

          } else if passBusy == false && passEnabled == true {  // todos os servidores estão ocupados

          } else if passBusy == true && passEnabled == false {  // todos os servidores estão desabilitados

          } else if passBusy == true && passEnabled == true {   // há servidores livres

            externalServerUrl  = mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Url
            externalServerName = mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name

          }

          fmt.Printf("loading [ %v ]: %v\n", externalServerName, externalServerUrl)

          containerUrl, err := url.Parse(externalServerUrl)
          if err != nil {
            //w.Write([]byte(err.Error()))
            loopCounter += 1

            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorCounter += 1
            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter += 1
            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].LastLoopError = true

            if mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter >= mainRoute.ConsecutiveErrorsToDisable {
              mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Enabled = false
              mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].DisabledSince = now

              fmt.Printf( "[ %v ] foi desabilitado após %v erros.\n", mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter )
            }

            continue
          }

          transport := &transport{http.DefaultTransport, nil}
          proxy := NewSingleHostReverseProxy(containerUrl)
          proxy.Transport = transport
          proxy.ServeHTTP(w, r)

          if transport.Error != nil {
            loopCounter += 1

            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorCounter += 1
            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter += 1
            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].LastLoopError = true

            if mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter >= mainRoute.ConsecutiveErrorsToDisable {
              mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Enabled = false
              mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].DisabledSince = now

              fmt.Printf( "[ %v ] foi desabilitado após %v erros.\n", mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter )
            }
            continue
          }

          // rodou sem erro

          mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter = 0
          mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].UsedSuccessfully += 1
          mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].TotalTime += time.Since( start ) * time.Nanosecond

          // LastLoopError evita um loop infinito em rotas com erro de resposta
          for keyUrl := range mainRoute.Routes[ keyRoute ].ProxyServers{
            mainRoute.Routes[ keyRoute ].ProxyServers[ keyUrl ].LastLoopError = false
          }

          return
        }
      }
    }
  }

  return

  /*cookie, _ := r.Cookie(sessionName)
  if cookie == nil {
    expiration := time.Now().Add(365 * 24 * time.Hour)
    cookie := http.Cookie{Name: sessionName, Value: sessionId(), Expires: expiration}
    http.SetCookie(w, &cookie)
  }

  cookie, _ = r.Cookie(sessionName)
  fmt.Printf("cookie: %q\n", cookie)*/

  urlModules := make( map[string]string )
  queryString := make( map[string][]string )
  regExDomainText   := `^(?P<subDomain>[a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9-.]*?[a-zA-Z0-9])[.]*(?P<domain>[A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9-]*[A-Za-z0-9]):*(?P<port>[0-9]*)$`

  matched, err := regexp.MatchString(regExDomainText, r.Host)
  if err != nil {}
  if matched == true {
    re := regexp.MustCompile(regExDomainText)
    fmt.Printf("all: %q\n", re.FindAllString(r.Host, -1))
    fmt.Printf("subDomain: %q\n", re.ReplaceAllString(r.Host,"${subDomain}"))
    fmt.Printf("domain: %q\n", re.ReplaceAllString(r.Host, "${domain}"))
    fmt.Printf("port: %q\n\n", re.ReplaceAllString(r.Host, "${port}"))
  }

  regExUser := `^/(?P<controller>[a-z0-9-]+)/(?P<module>[a-z0-9-]+)/(?P<site>.+?)/*$`
  matched, err = regexp.MatchString(regExUser, r.URL.Path)
  if matched == true {
    re := regexp.MustCompile(regExUser)
    for k, v := range re.SubexpNames() {
      if k == 0 {
        continue
      }

      urlModules[v] = re.ReplaceAllString(r.URL.Path, `${` + v + `}`)
    }
  }
  fmt.Printf("values: %q\n", urlModules)

  queryString, e := url.ParseQuery(r.URL.RawQuery)
  if e != nil {
    fmt.Printf("error: %v\n", e.Error())
  }
  fmt.Printf("query string: %q\n", queryString)



  fmt.Printf("Host: %v\n", r.Host)
  fmt.Printf("Method: %v\n", r.Method)
  fmt.Printf("RemoteAddr: %v\n", r.RemoteAddr)
  fmt.Printf("r.URL.Path: %v\n", r.URL.Path)
  fmt.Printf("r.URL.Host: %v\n", r.URL.Host)
  fmt.Printf("r.URL.RawPath: %v\n", r.URL.RawPath)
  fmt.Printf("r.URL.RawQuery: %v\n", r.URL.RawQuery)
  fmt.Printf("r.URL.Scheme: %v\n\n\n", r.URL.Scheme)
}

func timeMensure( start time.Time, name string ) {
  elapsed := time.Since(start)
  log.Printf("%s: %s", name, elapsed)
}

type transport struct {
  http.RoundTripper
  Error       error
}

func (t *transport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
  resp, err = t.RoundTripper.RoundTrip(req)
  if err != nil {
    t.Error = err
    return nil, err
  }
  b, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    return nil, err
  }
  err = resp.Body.Close()
  if err != nil {
    return nil, err
  }
  //b = bytes.Replace(b, []byte("server"), []byte("schmerver"), -1)
  body := ioutil.NopCloser(bytes.NewReader(b))
  resp.Body = body
  resp.ContentLength = int64(len(b))
  resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
  return resp, nil
}

// onExitFlushLoop is a callback set by tests to detect the state of the
// flushLoop() goroutine.
var onExitFlushLoop func()

// ReverseProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the
// client.
type ReverseProxy struct {
  // Director must be a function which modifies
  // the request into a new request to be sent
  // using Transport. Its response is then copied
  // back to the original client unmodified.
  // Director must not access the provided Request
  // after returning.
  Director func(*http.Request)

  // The transport used to perform proxy requests.
  // If nil, http.DefaultTransport is used.
  Transport http.RoundTripper

  // FlushInterval specifies the flush interval
  // to flush to the client while copying the
  // response body.
  // If zero, no periodic flushing is done.
  FlushInterval time.Duration

  // ErrorLog specifies an optional logger for errors
  // that occur when attempting to proxy the request.
  // If nil, logging goes to os.Stderr via the log package's
  // standard logger.
  ErrorLog *log.Logger

  // BufferPool optionally specifies a buffer pool to
  // get byte slices for use by io.CopyBuffer when
  // copying HTTP response bodies.
  BufferPool BufferPool

  // ModifyResponse is an optional function that
  // modifies the Response from the backend.
  // If it returns an error, the proxy returns a StatusBadGateway error.
  ModifyResponse func(*http.Response) error
}

// A BufferPool is an interface for getting and returning temporary
// byte slices for use by io.CopyBuffer.
type BufferPool interface {
  Get() []byte
  Put([]byte)
}

func singleJoiningSlash(a, b string) string {
  aslash := strings.HasSuffix(a, "/")
  bslash := strings.HasPrefix(b, "/")
  switch {
  case aslash && bslash:
    return a + b[1:]
  case !aslash && !bslash:
    return a + "/" + b
  }
  return a + b
}

// NewSingleHostReverseProxy returns a new ReverseProxy that routes
// URLs to the scheme, host, and base path provided in target. If the
// target's path is "/base" and the incoming request was for "/dir",
// the target request will be for /base/dir.
// NewSingleHostReverseProxy does not rewrite the Host header.
// To rewrite Host headers, use ReverseProxy directly with a custom
// Director policy.
func NewSingleHostReverseProxy(target *url.URL) *ReverseProxy {
  targetQuery := target.RawQuery
  director := func(req *http.Request) {
    req.URL.Scheme = target.Scheme
    req.URL.Host = target.Host
    req.URL.Path = singleJoiningSlash(target.Path, req.URL.Path)
    if targetQuery == "" || req.URL.RawQuery == "" {
      req.URL.RawQuery = targetQuery + req.URL.RawQuery
    } else {
      req.URL.RawQuery = targetQuery + "&" + req.URL.RawQuery
    }
    if _, ok := req.Header["User-Agent"]; !ok {
      // explicitly disable User-Agent so it's not set to default value
      req.Header.Set("User-Agent", "")
    }
  }
  return &ReverseProxy{Director: director}
}

func copyHeader(dst, src http.Header) {
  for k, vv := range src {
    for _, v := range vv {
      dst.Add(k, v)
    }
  }
}

func cloneHeader(h http.Header) http.Header {
  h2 := make(http.Header, len(h))
  for k, vv := range h {
    vv2 := make([]string, len(vv))
    copy(vv2, vv)
    h2[k] = vv2
  }
  return h2
}

// Hop-by-hop headers. These are removed when sent to the backend.
// http://www.w3.org/Protocols/rfc2616/rfc2616-sec13.html
var hopHeaders = []string{
  "Connection",
  "Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
  "Keep-Alive",
  "Proxy-Authenticate",
  "Proxy-Authorization",
  "Te",      // canonicalized version of "TE"
  "Trailer", // not Trailers per URL above; http://www.rfc-editor.org/errata_search.php?eid=4522
  "Transfer-Encoding",
  "Upgrade",
}

func (p *ReverseProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
  transport := p.Transport
  if transport == nil {
    transport = http.DefaultTransport
  }

  ctx := req.Context()
  if cn, ok := rw.(http.CloseNotifier); ok {
    var cancel context.CancelFunc
    ctx, cancel = context.WithCancel(ctx)
    defer cancel()
    notifyChan := cn.CloseNotify()
    go func() {
      select {
      case <-notifyChan:
        cancel()
      case <-ctx.Done():
      }
    }()
  }

  outreq := req.WithContext(ctx) // includes shallow copies of maps, but okay
  if req.ContentLength == 0 {
    outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
  }

  outreq.Header = cloneHeader(req.Header)

  p.Director(outreq)
  outreq.Close = false

  // Remove hop-by-hop headers listed in the "Connection" header.
  // See RFC 2616, section 14.10.
  if c := outreq.Header.Get("Connection"); c != "" {
    for _, f := range strings.Split(c, ",") {
      if f = strings.TrimSpace(f); f != "" {
        outreq.Header.Del(f)
      }
    }
  }

  // Remove hop-by-hop headers to the backend. Especially
  // important is "Connection" because we want a persistent
  // connection, regardless of what the client sent to us.
  for _, h := range hopHeaders {
    if outreq.Header.Get(h) != "" {
      outreq.Header.Del(h)
    }
  }

  if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
    // If we aren't the first proxy retain prior
    // X-Forwarded-For information as a comma+space
    // separated list and fold multiple headers into one.
    if prior, ok := outreq.Header["X-Forwarded-For"]; ok {
      clientIP = strings.Join(prior, ", ") + ", " + clientIP
    }
    outreq.Header.Set("X-Forwarded-For", clientIP)
  }

  res, err := transport.RoundTrip(outreq)
  if err != nil {
    p.logf("http: proxy error: %v", err)
    /* comentado por kemper para poder usar o load balance */
    //rw.WriteHeader(http.StatusBadGateway)
    return
  }

  // Remove hop-by-hop headers listed in the
  // "Connection" header of the response.
  if c := res.Header.Get("Connection"); c != "" {
    for _, f := range strings.Split(c, ",") {
      if f = strings.TrimSpace(f); f != "" {
        res.Header.Del(f)
      }
    }
  }

  for _, h := range hopHeaders {
    res.Header.Del(h)
  }

  if p.ModifyResponse != nil {
    if err := p.ModifyResponse(res); err != nil {
      p.logf("http: proxy error: %v", err)
      /* comentado por kemper para poder usar o load balance */
      //rw.WriteHeader(http.StatusBadGateway)
      return
    }
  }

  copyHeader(rw.Header(), res.Header)

  // The "Trailer" header isn't included in the Transport's response,
  // at least for *http.Transport. Build it up from Trailer.
  announcedTrailers := len(res.Trailer)
  if announcedTrailers > 0 {
    trailerKeys := make([]string, 0, len(res.Trailer))
    for k := range res.Trailer {
      trailerKeys = append(trailerKeys, k)
    }
    rw.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
  }

  rw.WriteHeader(res.StatusCode)
  if len(res.Trailer) > 0 {
    // Force chunking if we saw a response trailer.
    // This prevents net/http from calculating the length for short
    // bodies and adding a Content-Length.
    if fl, ok := rw.(http.Flusher); ok {
      fl.Flush()
    }
  }
  p.copyResponse(rw, res.Body)
  res.Body.Close() // close now, instead of defer, to populate res.Trailer

  if len(res.Trailer) == announcedTrailers {
    copyHeader(rw.Header(), res.Trailer)
    return
  }

  for k, vv := range res.Trailer {
    k = http.TrailerPrefix + k
    for _, v := range vv {
      rw.Header().Add(k, v)
    }
  }
}

func (p *ReverseProxy) copyResponse(dst io.Writer, src io.Reader) {
  if p.FlushInterval != 0 {
    if wf, ok := dst.(writeFlusher); ok {
      mlw := &maxLatencyWriter{
        dst:     wf,
        latency: p.FlushInterval,
        done:    make(chan bool),
      }
      go mlw.flushLoop()
      defer mlw.stop()
      dst = mlw
    }
  }

  var buf []byte
  if p.BufferPool != nil {
    buf = p.BufferPool.Get()
  }
  p.copyBuffer(dst, src, buf)
  if p.BufferPool != nil {
    p.BufferPool.Put(buf)
  }
}

func (p *ReverseProxy) copyBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
  if len(buf) == 0 {
    buf = make([]byte, 32*1024)
  }
  var written int64
  for {
    nr, rerr := src.Read(buf)
    if rerr != nil && rerr != io.EOF && rerr != context.Canceled {
      p.logf("httputil: ReverseProxy read error during body copy: %v", rerr)
    }
    if nr > 0 {
      nw, werr := dst.Write(buf[:nr])
      if nw > 0 {
        written += int64(nw)
      }
      if werr != nil {
        return written, werr
      }
      if nr != nw {
        return written, io.ErrShortWrite
      }
    }
    if rerr != nil {
      return written, rerr
    }
  }
}

func (p *ReverseProxy) logf(format string, args ...interface{}) {
  if p.ErrorLog != nil {
    p.ErrorLog.Printf(format, args...)
  } else {
    log.Printf(format, args...)
  }
}

type writeFlusher interface {
  io.Writer
  http.Flusher
}

type maxLatencyWriter struct {
  dst     writeFlusher
  latency time.Duration

  mu   sync.Mutex // protects Write + Flush
  done chan bool
}

func (m *maxLatencyWriter) Write(p []byte) (int, error) {
  m.mu.Lock()
  defer m.mu.Unlock()
  return m.dst.Write(p)
}

func (m *maxLatencyWriter) flushLoop() {
  t := time.NewTicker(m.latency)
  defer t.Stop()
  for {
    select {
    case <-m.done:
      if onExitFlushLoop != nil {
        onExitFlushLoop()
      }
      return
    case <-t.C:
      m.mu.Lock()
      m.dst.Flush()
      m.mu.Unlock()
    }
  }
}

func (m *maxLatencyWriter) stop() { m.done <- true }
