# Reverse proxy based on go's own code

Eu procurei muitos proxy reversos feitos em gó e achei alguns projetos bem complexos e complicados até que descobri que o próprio go fornece um proxy reverso simples e funcional, porém, que não tem um bom tratamento de erro, por isto, eu copiei o código original e fiz uma pequena mudança para que o mesmo atenda as minhas necessidades.

### Funcionamento

Imagine que você deveria instalar três containers em servidores diferentes para a mesma rota, mas, por algum motivo, você instalou apenas um container e as outras duas rotas não respondem.

O exemplo abaixo instala o blog ghost na sua própria máquina.

```
$ docker pull ghost
$ docker run -d --name ghostRoute1 -p 2368:2368 ghost
```

> Dica de ouro, instale esse gerenciador de containers para ajudar
```
$ docker run -it -v /var/run/docker.sock:/var/run/docker.sock moncho/dry
```
O proxy reverso vai tantar acessar a primeira rota, detectar o erro e seguir para a próxima rota até que uma rota responda.

Ele foi pensado para as seguintes situações:

* Todas as rotas respondem: a próxima rota é sempre chamada e todas as rotas são chamadas na mesma quantidade de vezes;
* Um rota responde erro esporadicamente: o erro é  ignorado e a próxima rota da lista é chamada até que uma rota responda sem erro;
* Uma rota responde erro consecutivamente: depois que uma rota apresenta x erros consecutivos, a mesma é removida por 90 segundos na esperança do servidor voltar.
* Todas as rotas retornaram erros consecutivos: caso todas as rotas estejam desabilitadas por erro, todas as rotas são habilitadas e testadas na esperança de funcionar.
* Todas as rotas falharam depois de mais de x tentativas seguidas na mesma chamada: uma página de erro é exibida.

### Configuração

```
  ProxyRootConfig = ProxyConfig{
    // Coloca o proxy na porta 8888
    ListenAndServe: ":8888",
    Routes: []ProxyRoute{
      // Monta um blog com o Ghost
      // Como o container tem seu próprio servidor, o mesmo gerencia erros e page not found
      {
        Name: "blog",
        // Determina o domínio onde o blog vai rodar
        Domain: ProxyDomain{
          SubDomain: "blog",
          Domain: "localhost",
          Port: "8888",
        },
        // Habilita a funcionalidade de proxy reverso
        ProxyEnable: true,
        // Determina todos os containers
        ProxyServers: []ProxyUrl{
          // esta rota funciona e foi mostrada lá em cima no texto
          {
            Name: "docker 1 - ok",
            Url: "http://localhost:2368",
          },
          // esta rota não funciona e será desabilitada
          {
            Name: "docker 2 - error",
            Url: "http://localhost:2367",
          },
          // esta rota não funciona e será desabilitada
          {
            Name: "docker 3 - error",
            Url: "http://localhost:2367",
          },
        },
      },
      // Crie sua própria página interna
      {
        Name: "hello",
        Domain: ProxyDomain{
          // opcional
          NotFoundHandle: ProxyRootConfig.ProxyNotFound,
          // opcional
          ErrorHandle: ProxyRootConfig.ProxyError,
          SubDomain: "",
          Domain: "localhost",
          Port: "8888",
        },
        // Expressão regular é lenta, principalmente em go. Use com moderação
        Path: ProxyPath{
          ExpReg: `^/(?P<controller>[a-z0-9-]+)/(?P<module>[a-z0-9-]+)/(?P<site>[a-z0-9]+.(htm|html))$`,
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: hello,
        },
      },
      // Adiciona uma nova rota, mas, você deve fazer sua própria segurança
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
        },
        ProxyEnable: false,
        Handle: handle{
          Handle: ProxyRootConfig.RouteAdd,
        },
      },
      // Remove uma nova rota, mas, você deve fazer sua própria segurança
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
      // Mostra um sjon com os dados de todas as rotas
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
```
