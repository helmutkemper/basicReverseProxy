package main

import (
  log "github.com/helmutkemper/seelog"
  "net/http"
  "net/url"
  "regexp"
  "time"
  "net"
  "sync"
  "strings"
  "context"
  "io"
  "encoding/json"
  "bytes"
  "strconv"
  "io/ioutil"
  "fmt"
  "os"
  "errors"
)

var ProxyRootConfig ProxyConfig
var ProxyNewRootConfig []ProxyRoute

func hello(w ProxyResponseWriter, r *ProxyRequest) {
  w.Header().Set("Content-Type", "text/html; charset=utf-8")

  w.Write( []byte( "controller: " ) )
  w.Write( []byte( r.ExpRegMatches[ "controller" ] ) )
  w.Write( []byte( "<br>" ) )

  w.Write( []byte( "module: " ) )
  w.Write( []byte( r.ExpRegMatches[ "module" ] ) )
  w.Write( []byte( "<br>" ) )

  w.Write( []byte( "site: " ) )
  w.Write( []byte( r.ExpRegMatches[ "site" ] ) )
  w.Write( []byte( "<br>" ) )
}

func main() {
  ProxyRootConfig = ProxyConfig{
    ListenAndServe: ":8080",
    ErrorHandle: ProxyRootConfig.ProxyError,
    NotFoundHandle: ProxyRootConfig.ProxyNotFound,
    MaxLoopTry: 20,
    ConsecutiveErrorsToDisable: 10,
    TimeToKeepDisabled: time.Second * 90,
    TimeToVerifyDisabled: time.Second * 30,
    Routes: []ProxyRoute{
      {
        Name: "blog",
        Domain: ProxyDomain{
          SubDomain: "blog",
          Domain: "localhost",
          Port: "8888",
        },
        ProxyEnable: true,
        ProxyServers: []ProxyUrl{
          {
            Name: "docker 1 - ok",
            Url: "http://localhost:2368",
          },
          {
            Name: "docker 2 - error",
            Url: "http://localhost:2367",
          },
          {
            Name: "docker 3 - error",
            Url: "http://localhost:2367",
          },
        },
      },
      {
        Name: "hello",
        Domain: ProxyDomain{
          NotFoundHandle: ProxyRootConfig.ProxyNotFound,
          ErrorHandle: ProxyRootConfig.ProxyError,
          SubDomain: "",
          Domain: "localhost",
          Port: "8888",
        },
        Path: ProxyPath{
          ExpReg: `^/(?P<controller>[a-z0-9-]+)/(?P<module>[a-z0-9-]+)/(?P<site>[a-z0-9]+.(htm|html))$`,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: hello,
        },
      },
      {
        Name: "addTest",
        Domain: ProxyDomain{
          NotFoundHandle: ProxyRootConfig.ProxyNotFound,
          ErrorHandle: ProxyRootConfig.ProxyError,
          SubDomain: "",
          Domain: "localhost",
          Port: "8888",
        },
        Path: ProxyPath{
          Path : "/add",
          Method: "POST",
          //ExpReg: `^/(?P<controller>[a-z0-9-]+)/(?P<module>[a-z0-9-]+)/(?P<site>[a-z0-9]+.(htm|html))$`,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: ProxyRootConfig.RouteAdd,
        },
      },
      {
        Name: "removeTest",
        Domain: ProxyDomain{
          NotFoundHandle: ProxyRootConfig.ProxyNotFound,
          ErrorHandle: ProxyRootConfig.ProxyError,
          SubDomain: "",
          Domain: "localhost",
          Port: "8888",
        },
        Path: ProxyPath{
          Path : "/remove",
          Method: "POST",
          //ExpReg: `^/(?P<controller>[a-z0-9-]+)/(?P<module>[a-z0-9-]+)/(?P<site>[a-z0-9]+.(htm|html))$`,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: ProxyRootConfig.RouteDelete,
        },
      },
      {
        Name: "panel",
        Domain: ProxyDomain{
          NotFoundHandle: ProxyRootConfig.ProxyNotFound,
          ErrorHandle: ProxyRootConfig.ProxyError,
          SubDomain: "root",
          Domain: "localhost",
          Port: "8888",
        },
        Path: ProxyPath{
          Path: "/statistics",
          Method: "GET",
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: ProxyRootConfig.ProxyStatistics,
        },
      },
    },
  }
  ProxyRootConfig.Prepare()
  go ProxyRootConfig.VerifyDisabled()

  http.HandleFunc("/", ProxyFunc)
  http.ListenAndServe(ProxyRootConfig.ListenAndServe, nil)
}

type ProxyHandlerFunc func(ProxyResponseWriter, *ProxyRequest)

type ProxyUrl struct {
  /*
  Url da rota para o proxy
  */
  Url                             string                  `json:"url"`

  /*
  Nome da rota para manter organizado
  */
  Name                            string                  `json:"name"`

  /*
  Tempo total de execução da rota.
  A soma de todos os tempos de resposta
  */
  TotalTime                       time.Duration           `json:"totalTime"`

  /*
  Quantidades de usos sem erro
  */
  UsedSuccessfully                int64                   `json:"usedSuccessfully"`

  /*
  Habilitada / Desabilitada temporariamente para esperar a rota voltar a responder
  */
  Enabled                         bool                    `json:"enabled"`

  /*
  Quando marcado true, a rotina que reabita ignora a rota e a mantém desabilitada
  */
  Forever                         bool                    `json:"forever"`

  /*
  Total de erros durante a execução da rota do proxy
  */
  ErrorCounter                    int64                   `json:"errorCounter"`

  /*
  Conta quantos erros seguidos houveram para poder decidir se desabilita a roda do proxy
  */
  ErrorConsecutiveCounter         int64                   `json:"errorConsecutiveCounter"`

  /*
  Arquiva o tempo desabilitado para poder reabilitar por time out
  */
  DisabledSince                   time.Time               `json:"-"`

  /*
  Usado pelo código para evitar que uma rota fique em loop infinito
  */
  LastLoopError                   bool                    `json:"lastLoopError"`

  /*
  Usado para indicar que a rota já foi usada
  A ideia é escolher a próximo rota livre em vez de ficar repetindo
  */
  LastLoopOk                      bool                    `json:"lastLoopOk"`
}

type ProxyRoute struct {
  /*
  Nome para o log e outras funções, deve ser único e começar com letra ou '_'
  */
  Name                            string                  `json:"name"`

  /*
  Dados do domínio
  */
  Domain                          ProxyDomain             `json:"domain"`

  /*
  [opcional] Dados do caminho dentro do domínio
  */
  Path                            ProxyPath                `json:"path"`

  /*
  [opcional] Dado da aplicação local
  */
  Handle                          handle                  `json:"handle"`

  /*
  Habilita a funcionalidade do proxy, caso contrário, será chamada a função handle
  */
  ProxyEnable                     bool                    `json:"proxyEnable"`

  /*
  Lista de todas as URLs para os containers com a aplicação
  */
  ProxyServers                    []ProxyUrl              `json:"proxyServers"`
}
type ProxyDomain struct {

  /*
  Função de erro padrão para o domínio.
  */
  ErrorHandle                     ProxyHandlerFunc        `json:"-"`

  /*
  Função de page not found padrão para o domínio
  */
  NotFoundHandle                  ProxyHandlerFunc        `json:"-"`

  /*
  [opcional] sub domínio sem ponto final. Ex.: blog.domínio.com fica apenas blog
  */
  SubDomain                       string                  `json:"subDomain"`

  /*
  Domínio onde o sistema roda. Foi imaginado para ser textual, por isto, evite ip address
  */
  Domain                          string                  `json:"domain"`

  /*
  [opcional] Coloque apenas o número da porta, sem os ':'. Ex. :8080, fica apenas 8080
  */
  Port                            string                  `json:"port"`
}

type ProxyPath struct {
  /*
  [opcional] Quando omitido, juntamente com ExpReg, faz com que todo o subdomínio seja usado para a rota
  */
  Path                            string                  `json:"path"`

  /*
  [opcional] Método da chamada GET/POST/DELETE...
  */
  Method                          string                  `json:"method"`

  /*
  true faz com que o path seja checado por expressão regular
  */
  ExpReg                          string                  `json:"expReg"`
}

type handle struct {
  /*
  Nome da rota para manter organizado
  */
  Name                            string                  `json:"name"`

  /*
  Tempo total de execução da rota.
  A soma de todos os tempos de resposta
  */
  TotalTime                       time.Duration           `json:"totalTime"`

  /*
  Quantidades de usos sem erro
  */
  UsedSuccessfully                int64                   `json:"usedSuccessfully"`

  /*
  Função a ser servida
  */
  Handle                          ProxyHandlerFunc        `json:"-"`
}

type ProxyRsaStt struct {
  PublicKey                       string                  `bson:"publicKeyPath"`
  PublicPen                       string                  `bson:"publicPenPath"`
  PrivateKey                      string                  `bson:"privateKeyPath"`
  PrivatePen                      string                  `bson:"privatePenPath"`
  BitSize                         int                     `bson:"bitSize"`
}

type ProxyConfig struct {
  /*
  Configuração do seelog
  @see https://github.com/cihub/seelog
  */
  SeeLogConfig                    string                  `json:"seeLogConfig"`

  /*
  Configuração das chaves {{incompleto!}}
  */
  Rsa                             ProxyRsaStt             `json:"rsa"`

  /*
  Expressão regular que identifica o domínio do site
  */
  DomainExpReg                    string                  `json:"domainExpReg"`

  /*
  Função de erro genérica, caso a função do domínio não seja definida
  */
  ErrorHandle                     ProxyHandlerFunc        `json:"-"`
  
  /*
  Função de page not found genérica, caso a função do domínio não seja definida
  */
  NotFoundHandle                  ProxyHandlerFunc        `json:"-"`

  /*
  Tamanho de caracteres do token de segurança
  */
  UniqueIdLength                  int                     `json:"uniqueIdLength"`

  /*
  URL do servidor principal
  */
  ListenAndServe                  string                  `json:"listenAndServe"`

  /*
  Quantidade máxima de loop quando todas as rotas do proxy falham
  */
  MaxLoopTry                      int
  
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
  Routes                          []ProxyRoute
}

/*
Esta função adiciona novas rotas ao proxy
{
    "name": "news",
    "domain": {
      "subDomain": "news",
      "domain": "localhost",
      "port": "8888"
    },
    "proxyEnable": true,
    "proxyServers": [
    {
          "name": "docker 1 - ok",
          "url": "http://localhost:2368"
    },
    {
          "name": "docker 2 - ok",
          "url": "http://localhost:2368"
    },
    {
      "name": "docker 3 - ok",
          "url": "http://localhost:2368"
    }
  ]
}
*/
func(el *ProxyConfig)RouteAdd(w ProxyResponseWriter, r *ProxyRequest) {
  // Esta função coloca a rota nova em 'ProxyNewRootConfig' e espera uma nova chamada em uma rota qualquer para que a 
  // nova rota tenha efeito. Isso é transparente para o usuário final, mas, a rota não pode entrar em vigor durante o
  // processamento da rota anterior, ou o sistema trava, devido a mudança dos 'ponteiros'
  
  var newRoute ProxyRoute
  var output = JSonOutStt{}

  if len(ProxyNewRootConfig) != 0 {
    ProxyRootConfig.Routes = ProxyNewRootConfig
  }

  err := json.NewDecoder(r.Body).Decode(&newRoute)

  if err != nil {
    output.ToOutput(0, err, []int{}, w)
    return
  }

  if newRoute.ProxyEnable == false {
    output.ToOutput(0, errors.New("this function only adds new routes that can be used in conjunction with the reverse proxy"), []int{}, w)
    return
  }

  if len( newRoute.ProxyServers ) == 0 {
    output.ToOutput(0, errors.New("this function must receive at least one route that can be used in conjunction with the reverse proxy"), []int{}, w)
    return
  }

  for _, route := range newRoute.ProxyServers {
    if route.Name == "" {
      output.ToOutput(0, errors.New("every route must have a name assigned to it"), []int{}, w)
      return
    }

    _, err := url.Parse(route.Url)
    if err != nil {
      output.ToOutput(0, errors.New("the route of name '" + route.Name + "' presented the following error: " + err.Error()), []int{}, w)
      return
    }
  }

  // Habilita todas as rotas, pois, o padrão do go é false
  for urlKey := range newRoute.ProxyServers {
    newRoute.ProxyServers[ urlKey ].Enabled = true
  }
  
  ProxyNewRootConfig = append(ProxyRootConfig.Routes, newRoute)
  output.ToOutput(int64( len( ProxyNewRootConfig ) ), nil, ProxyNewRootConfig, w)
}

/*
Esta função elimina rotas do proxy
{
    "name": "name_of_route"
}
*/
func(el *ProxyConfig)RouteDelete(w ProxyResponseWriter, r *ProxyRequest) {
  // Esta função coloca a rota nova em 'ProxyNewRootConfig' e espera uma nova chamada em uma rota qualquer para que a 
  // nova rota tenha efeito. Isso é transparente para o usuário final, mas, a rota não pode entrar em vigor durante o
  // processamento da rota anterior, ou o sistema trava, devido a mudança dos 'ponteiros'

  var newRoute ProxyRoute
  var output = JSonOutStt{}

  err := json.NewDecoder(r.Body).Decode(&newRoute)

  if err != nil {
    output.ToOutput(0, err, []int{}, w)
    return
  }

  var i int
  nameFound := false
  for i = 0; i != len( ProxyRootConfig.Routes ); i += 1 {
    if ProxyRootConfig.Routes[i].Name == newRoute.Name {
      nameFound = true
      break
    }
  }

  if nameFound == true {
    if i == 0 {
      ProxyNewRootConfig = ProxyRootConfig.Routes[1:]
    } else if i == len(ProxyRootConfig.Routes) - 1 {
      ProxyNewRootConfig = ProxyRootConfig.Routes[:len(ProxyRootConfig.Routes)-1]
    } else {
      ProxyNewRootConfig = append( ProxyRootConfig.Routes[0:i], ProxyRootConfig.Routes[i+1:]... )
    }
  }

  if ProxyRootConfig.Routes[i].ProxyEnable == false {
    output.ToOutput(0, errors.New("this function can only remove the routes used with the reverse proxy, not being able to remove other types of routes"), []int{}, w)
    return
  }

  b, e := json.Marshal(ProxyNewRootConfig)
  if e != nil {
    w.Write([]byte(e.Error()))
    return
  }
  w.Write(b)

  output.ToOutput(int64( len( ProxyNewRootConfig ) ), nil, ProxyNewRootConfig, w)
}

// Inicializa algumas variáveis
func(el *ProxyConfig)Prepare(){

  // Cria a pasta de log
  logPath := "./log"
  if _, err := os.Stat( logPath ); os.IsNotExist( err ) {
    err = os.Mkdir( logPath, 0777 )
  }

  // Configura o log como arquivos com tamanho limitado. Um arquivo, info.log para coisas simples e um arquivo warn.log
  // para coisas que devem ser observadas pelo administrador
  if el.SeeLogConfig == "" {
    el.SeeLogConfig = `<seelog minlevel="warn" maxlevel="critical" type="sync">
  <outputs formatid="all">
    <filter levels="trace">
      <rollingfile type="size" filename="` + logPath + `/info.log" maxrolls="2" maxsize="10000" />
    </filter>
    <filter levels="debug">
      <rollingfile type="size" filename="` + logPath + `/info.log" maxrolls="2" maxsize="10000" />
    </filter>
    <filter levels="info">
      <rollingfile type="size" filename="` + logPath + `/info.log" maxrolls="2" maxsize="10000" />
    </filter>
    <filter levels="warn">
      <rollingfile type="size" filename="` + logPath + `/warn.log" maxrolls="2" maxsize="10000" />
      <console/>
    </filter>
    <filter levels="error">
      <rollingfile type="size" filename="` + logPath + `/warn.log" maxrolls="2" maxsize="10000" />
      <console/>
    </filter>
    <filter levels="critical">
      <rollingfile type="size" filename="` + logPath + `/warn.log" maxrolls="2" maxsize="10000" />
      <console/>
    </filter>
  </outputs>
  <formats>
    <format id="all" format="[%Level::%Date %Time] %Msg%n"/>
  </formats>
</seelog>`
  }

  logger, err := log.LoggerFromConfigAsBytes([]byte(el.SeeLogConfig))
  if err != nil {
    fmt.Printf( "Erro na configuração do log. Error: %v", err.Error() )
  }
  log.UseLogger(logger)

  // Define o tamanho do token como sendo 30 caracteres
  if el.UniqueIdLength == 0 {
    el.UniqueIdLength = 30
  }

  // Expressão regular do domínio
  if el.DomainExpReg == "" {
    el.DomainExpReg = `^(?P<subDomain>[a-zA-Z0-9]??|[a-zA-Z0-9]?[a-zA-Z0-9-.]*?[a-zA-Z0-9]*)[.]*(?P<domain>[A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9-]*[A-Za-z0-9]):*(?P<port>[0-9]*)$`
  }

  // Após 20 tentativas de acessar todos os containers da rota, uma mensagem de erro é exibida
  if el.MaxLoopTry == 0 {
    el.MaxLoopTry = 20
  }

  // Caso um container apresente mais de 10 erros consecutivos, o mesmo é desabilitado
  if el.ConsecutiveErrorsToDisable == 0 {
    el.ConsecutiveErrorsToDisable = 10
  }

  // Deixa um container desabilitado por 90 segundos após vários erros consecutivos
  if el.TimeToKeepDisabled == 0 {
    el.TimeToKeepDisabled = time.Second * 90
  }

  // Faz um teste a cada 30 segundos para saber se há containers desabilitados além do tempo
  if el.TimeToVerifyDisabled == 0 {
    el.TimeToVerifyDisabled = time.Second * 30
  }

  // Função de erro padrão do sistema
  if el.ErrorHandle == nil {
    el.ErrorHandle = el.ProxyError
  }

  // Função de page not found padrão do sistema
  if el.NotFoundHandle == nil {
    el.NotFoundHandle = el.ProxyNotFound
  }

  // Habilita todas as rotas do proxy, pois, o padrão do go é false
  for routesKey := range el.Routes{
    for urlKey := range el.Routes[ routesKey ].ProxyServers {
      el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled = true
    }
  }
}

func(el *ProxyConfig)ProxyError(w ProxyResponseWriter, r *ProxyRequest) {
  w.Write( []byte( `<html><header><style>body{height:100%; position:relative}div{margin:auto;height: 100%;width: 100%;position:fixed;top:0;bottom:0;left:0;right:0;background:blue;}div.center{margin:auto;height: 70%;width: 70%;}</style></header><body><div><div style="color:#ffff;" class="center"><p style="text-align: center; background-color: #888888;">There is something very wrong!</p><p>&nbsp;</p>The address is correct, but no server has responded correctly. The system administrator will be informed about this.<p>&nbsp;</p>Mussum Ipsum, cacilds vidis litro abertis. Interagi no mé, cursus quis, vehicula ac nisi. Viva Forevis aptent taciti sociosqu ad litora torquent. Atirei o pau no gatis, per gatis num morreus. Quem num gosta di mim que vai caçá sua turmis!</div></div></body></html>` ) )
}

func(el *ProxyConfig)ProxyNotFound(w ProxyResponseWriter, r *ProxyRequest) {
  w.Write( []byte( `<html><header><style>body{height:100%; position:relative}div{margin:auto;height: 100%;width: 100%;position:fixed;top:0;bottom:0;left:0;right:0;background:blue;}div.center{margin:auto;height: 70%;width: 70%;}</style></header><body><div><div style="color:#ffff;" class="center"><p style="text-align: center; background-color: #888888;">Page Not Found!</p><p>&nbsp;</p>Mussum Ipsum, cacilds vidis litro abertis. Interagi no mé, cursus quis, vehicula ac nisi. Viva Forevis aptent taciti sociosqu ad litora torquent. Atirei o pau no gatis, per gatis num morreus. Quem num gosta di mim que vai caçá sua turmis!<p>&nbsp;</p>Mussum Ipsum, cacilds vidis litro abertis. Interagi no mé, cursus quis, vehicula ac nisi. Viva Forevis aptent taciti sociosqu ad litora torquent. Atirei o pau no gatis, per gatis num morreus. Quem num gosta di mim que vai caçá sua turmis!</div></div></body></html>` ) )
}

// Verifica se há urls do proxy desabilitadas e as habilita depois de um tempo
// A ideia é que o servidor possa está fora do ar por um tempo, por isto, ele remove a rota por algum tempo, para evitar
// chamadas desnecessárias ao servidor
func(el *ProxyConfig)VerifyDisabled(){
  for {
    for routesKey := range el.Routes {
      for urlKey := range el.Routes[ routesKey ].ProxyServers {
        if time.Since(el.Routes[ routesKey ].ProxyServers[ urlKey ].DisabledSince) >= el.TimeToKeepDisabled && el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled == false && el.Routes[ routesKey ].ProxyServers[ urlKey ].Forever == false {
          el.Routes[ routesKey ].ProxyServers[ urlKey ].ErrorConsecutiveCounter = 0
          el.Routes[ routesKey ].ProxyServers[ urlKey ].Enabled = true
        }
      }
    }

    time.Sleep(el.TimeToVerifyDisabled)
  }
}

func(el *ProxyConfig)ProxyStatistics(w ProxyResponseWriter, r *ProxyRequest) {

  byteJSon, err := json.Marshal( ProxyRootConfig.Routes )
  if err != nil {
    w.Write( []byte( err.Error() ) )
    return
  }

  w.Write( byteJSon )
}

type MetaJSonOutStt struct{
  TotalCount  int64               `json:"TotalCount"`
  Error       string              `json:"Error"`
}

type JSonOutStt struct{
  Meta              MetaJSonOutStt      `json:"Meta"`
  Objects           interface{}         `json:"Objects"`
  geoJSonHasOutput  bool                `json:"-"`
}

func( el *JSonOutStt ) ToOutput( totalCountAInt int64, errorAErr error, dataATfc interface{}, w ProxyResponseWriter ) {
  var errorString = ""

  w.Header().Set( "Content-Type", "application/json; charset=utf-8" )

  if errorAErr != nil {
    w.WriteHeader(http.StatusInternalServerError)
    errorString = errorAErr.Error()
    totalCountAInt = 0
  } else {
    w.WriteHeader(http.StatusOK)
  }

  el.Meta = MetaJSonOutStt{
    Error: errorString,
    TotalCount: totalCountAInt,
  }

  if errorAErr != nil {
    el.Objects = []int{}
  } else {
    switch dataATfc.(type) {
    default:
      el.Objects = dataATfc
    }
  }

  if err := json.NewEncoder( w ).Encode( el ); err != nil {
    log.Warn( err )
  }
}

type ProxyResponseWriter struct {
  http.ResponseWriter
}

type ProxyRequest struct {
  *http.Request
  QueryString                     map[string][]string
  ExpRegMatches                   map[string]string
  SubDomain                       string
  Domain                          string
  Port                            string
  Path                            string
}

func ProxyFunc(w http.ResponseWriter, r *http.Request) {

  // Espera uma nova chamada para que a nova rota tenha efeito
  if len( ProxyNewRootConfig ) > 0 {
    ProxyRootConfig.Routes = ProxyNewRootConfig
    ProxyNewRootConfig = make([]ProxyRoute, 0)
  }

  var responseWriter = ProxyResponseWriter{
    ResponseWriter: w,
  }

  var request = &ProxyRequest{
    Request: r,
  }

  // a ideia era ter mais controle sobre o relógio, mas, terminou ficando inacabada
  now := time.Now()

  start := time.Now()

  var handleName string
  
  //defer não funcionou direito
  //defer timeMensure( start, handleName )

  request.ExpRegMatches = make( map[string]string )
  queryString := make( map[string][]string )
  
  // Trata o domínio e o separa
  // fixme: rever isto. isto é necessário? deixa o sistema mais lento
  matched, err := regexp.MatchString(ProxyRootConfig.DomainExpReg, r.Host)
  if err != nil {
    // há um erro grave na expreg do domínio
    log.Debugf( "The regular expression in charge of identifying the domain data has a serious error and the reverse proxy system can not continue. ExpReg: '/%v/' Error: %v", ProxyRootConfig.DomainExpReg, err.Error() )
    log.Criticalf( "The regular expression in charge of identifying the domain data has a serious error and the reverse proxy system can not continue. Error: %v", err.Error() )
    return
  }

  if matched == true {
    re := regexp.MustCompile(ProxyRootConfig.DomainExpReg)

    request.SubDomain = re.ReplaceAllString(r.Host,"${subDomain}")
    request.Domain = re.ReplaceAllString(r.Host, "${domain}")
    request.Port = re.ReplaceAllString(r.Host, "${port}")
  } else {
    // a equação de domínio não bateu
    log.Warnf( "Regular domain expression did not hit domain %v", r.Host )
    return
  }

  // trata a query string
  // fixme: isto é necessário aqui? deixa o sistema mais lento
  queryString, err = url.ParseQuery(r.URL.RawQuery)
  if err != nil {
    // há um erro na query string
    log.Infof( "The query string passed by the user does not appear to be in the correct format. Query String: %v Host: %v%v", r.URL.RawQuery, r.Host, r.URL.Path )
  }

  request.QueryString = queryString

  // Trata todas as rotas
  for keyRoute, route := range ProxyRootConfig.Routes {

    handleName = route.Name

    if route.Domain.SubDomain != "" {
      route.Domain.SubDomain += "."
    }

    if route.Domain.Port != "" {
      route.Domain.Port = ":" + route.Domain.Port
    }

    if r.Host != route.Domain.SubDomain + route.Domain.Domain + route.Domain.Port {
      continue
    }

    // O domínio foi encontrado
    if route.Path.ExpReg != "" && ( route.Path.Method == "" || route.Path.Method == r.Method ) {

      matched, err = regexp.MatchString(route.Path.ExpReg, r.URL.Path)
      if matched == true {
        re := regexp.MustCompile(route.Path.ExpReg)
        for k, v := range re.SubexpNames() {
          if k == 0 || v == "" {
            continue
          }

          request.ExpRegMatches[v] = re.ReplaceAllString(r.URL.Path, `${`+v+`}`)
        }

        if ProxyRootConfig.Routes[ keyRoute ].Handle.Handle != nil {
          ProxyRootConfig.Routes[ keyRoute ].Handle.Handle(responseWriter, request)
          ProxyRootConfig.Routes[ keyRoute ].Handle.TotalTime += time.Since( start ) * time.Nanosecond
          ProxyRootConfig.Routes[ keyRoute ].Handle.UsedSuccessfully += 1
          timeMensure( start, handleName )
          return
        }

      } else {
        continue
      }

    } else if ( route.Path.Method == "" || route.Path.Method == r.Method ) && ( route.Path.Path == r.URL.Path || route.Path.Path == "" ) {

      if ProxyRootConfig.Routes[ keyRoute ].Handle.Handle != nil {
        ProxyRootConfig.Routes[ keyRoute ].Handle.Handle(responseWriter, request)
        ProxyRootConfig.Routes[ keyRoute ].Handle.TotalTime += time.Since( start ) * time.Nanosecond
        ProxyRootConfig.Routes[ keyRoute ].Handle.UsedSuccessfully += 1
        timeMensure( start, handleName )
        return
      }

    // O domínio foi encontrado, porém, o path dentro do domínio não
    } else {
      continue
    }

    if route.ProxyEnable == false {
      ProxyRootConfig.Routes[ keyRoute ].Handle.Handle(responseWriter, request)
      ProxyRootConfig.Routes[ keyRoute ].Handle.TotalTime += time.Since( start ) * time.Nanosecond
      ProxyRootConfig.Routes[ keyRoute ].Handle.UsedSuccessfully += 1
      timeMensure( start, handleName )
      return
    }

    loopCounter := 0

    for {
      passEnabled := false
      keyUrlToUse := 0
      externalServerUrl := ""
      passNextRoute := false
      // Procura pela próxima rota para uso que esteja habilitada
      for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers{
        if ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopOk == false && ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].Enabled == true && ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopError == false {
          passNextRoute = true
          passEnabled = true
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopOk = true
          keyUrlToUse = urlKey
          break
        }
      }

      // A próxima rota não foi encontrada
      if passNextRoute == false {
        // Limpa todas as indicações de próxima rota
        for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers{
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopOk = false
        }

        // Procura por uma rota habilitada e que não houve um erro na tentativa anterior
        for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers {
          if ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].Enabled == true && ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopError == false {
            ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopOk = true
            passEnabled = true
            keyUrlToUse = urlKey
            break
          }
        }

        // Todas as rotas estão desabilitadas ou houveram erros na tentativa anterior
        if passEnabled == false {

          // Todas as rotas estão desabilitadas ou houveram erros na tentativa anterior
          log.Warnf( "All routes reported error on previous attempt or are disabled. Host: %v", r.Host )

          // Desabilita a indicação de erro na etapa anterior
          for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers {
            ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopError = false
          }

          // Procura por uma rota habilitada mesmo que tenha tido erro na etapa anterior
          // Uma rota desabilitada teve vários erros consecutivos, por isto, foi desabilitada temporariamente
          for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers {
            if ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].Enabled == true {
              ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].LastLoopOk = true
              passEnabled = true
              keyUrlToUse = urlKey
              break
            }
          }
        }
      }

      // Todas as rotas estão desabilitada por erro
      // Habilita todas as rotas e tenta novamente
      if passEnabled == false {
        for urlKey := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers{
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ urlKey ].Enabled = true
        }

        //aconteceu um erro grave, todas as rotas falharam com erros consecutivos e foram habilitadas a força para tentar de qualquer modo
        log.Warnf( "All %v domain routes are disabled by error and the system is trying all routes anyway.", r.Host )

        loopCounter += 1
        continue
      }

      externalServerUrl  = ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Url

      containerUrl, err := url.Parse(externalServerUrl)
      if err != nil {
        // Avisar que houve erro no parser
        log.Criticalf( "The route '%v - %v' of the domain '%v' is wrong. Error: %v", ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, externalServerUrl, r.Host, err.Error() )
        loopCounter += 1

        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorCounter += 1
        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter += 1
        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].LastLoopError = true

        if ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter >= ProxyRootConfig.ConsecutiveErrorsToDisable {

          // avisar que rota foi removida
          log.Criticalf( "The route '%v - %v' of the domain '%v' is wrong and has been disabled indefinitely until it is corrected by the admin.", ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, externalServerUrl, r.Host )

          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Enabled = false
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Forever = true
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].DisabledSince = now
        }

        // Houveram erros excessivos e o processo foi abortado
        if loopCounter >= ProxyRootConfig.MaxLoopTry {

          // Página de erro específica do domínio
          if ProxyRootConfig.Routes[ keyRoute ].Domain.ErrorHandle != nil {
            ProxyRootConfig.Routes[ keyRoute ].Domain.ErrorHandle(responseWriter, request)

          // Página de erro do sistema
          } else if ProxyRootConfig.ErrorHandle != nil {
            ProxyRootConfig.ErrorHandle(responseWriter, request)
          }

          timeMensure( start, handleName )
          return
        }

        continue
      }

      transport := &transport{
        RoundTripper: http.DefaultTransport,
        Error: nil,
      }
      proxy := NewSingleHostReverseProxy(containerUrl)
      proxy.Transport = transport
      proxy.ServeHTTP(w, r)

      if transport.Error != nil {
        // avisar que houve erro na leitura da rota
        log.Warnf( "The route '%v - %v' of the domain '%v' returned an error. Error: %v", ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, externalServerUrl, r.Host, transport.Error.Error() )
        loopCounter += 1

        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorCounter += 1
        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter += 1
        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].LastLoopError = true

        if ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter >= ProxyRootConfig.ConsecutiveErrorsToDisable {
          // avisar que rota foi removida
          log.Warnf( "The route '%v - %v' of the domain '%v' returned many consecutive errors and was temporarily disabled.", ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Name, externalServerUrl, r.Host )

          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].Enabled = false
          ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].DisabledSince = now
        }

        // Houveram erros excessivos e o processo foi abortado
        if loopCounter >= ProxyRootConfig.MaxLoopTry {

          log.Criticalf( "The '%v' domain returned more %v consecutive errors and the error page was displayed to the user.", r.Host, ProxyRootConfig.MaxLoopTry )

          // Página de erro específica do domínio
          if ProxyRootConfig.Routes[ keyRoute ].Domain.ErrorHandle != nil {
            ProxyRootConfig.Routes[ keyRoute ].Domain.ErrorHandle(responseWriter, request)

          // Página de erro do sistema
          } else if ProxyRootConfig.ErrorHandle != nil {
            ProxyRootConfig.ErrorHandle(responseWriter, request)
          }

          timeMensure( start, handleName )
          return
        }

        continue
      }

      // rodou sem erro

      ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].ErrorConsecutiveCounter = 0
      ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].UsedSuccessfully += 1
      ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrlToUse ].TotalTime += time.Since( start ) * time.Nanosecond

      // LastLoopError evita um loop infinito em rotas com erro de resposta
      for keyUrl := range ProxyRootConfig.Routes[ keyRoute ].ProxyServers{
        ProxyRootConfig.Routes[ keyRoute ].ProxyServers[ keyUrl ].LastLoopError = false
      }

      timeMensure( start, handleName )
      return
    }
  }

  // nenhum domínio bateu e está é uma página 404 genérica?
  if ProxyRootConfig.NotFoundHandle != nil {
    ProxyRootConfig.NotFoundHandle(responseWriter, request)
  }
  timeMensure( start, handleName )
  return

  /*cookie, _ := r.Cookie(sessionName)
  if cookie == nil {
    expiration := time.Now().Add(365 * 24 * time.Hour)
    cookie := http.Cookie{Name: sessionName, Value: sessionId(), Expires: expiration}
    http.SetCookie(w, &cookie)
  }

  cookie, _ = r.Cookie(sessionName)
  fmt.Printf("cookie: %q\n", cookie)*/
}

func timeMensure( start time.Time, name string ) {
  elapsed := time.Since(start)
  log.Infof("%s: %s", name, elapsed)
}

type transport struct {
  http.RoundTripper
  Error           error
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
  // ErrorLog *log.Logger

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
    //p.logf("http: proxy error: %v", err)
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
      //p.logf("http: proxy error: %v", err)
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
      //p.logf("httputil: ReverseProxy read error during body copy: %v", rerr)
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

/*func (p *ReverseProxy) logf(format string, args ...interface{}) {
  if p.ErrorLog != nil {
    p.ErrorLog.Printf(format, args...)
  } else {
    log.Printf(format, args...)
  }
}*/

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
