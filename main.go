package main

import (
	"fmt"

	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"
	"github.com/uber/jaeger-lib/metrics"
)

var (
	port = "8080"
	addr = ":8080"
)

func init() {
	cfg := jaegercfg.Configuration{
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans:          true,
			CollectorEndpoint: "http://localhost:14268/api/traces",
		},
	}
	_, err := cfg.InitGlobalTracer(
		"jaeger-example", // 服务名
		jaegercfg.Logger(jaegerlog.StdLogger),
		jaegercfg.Metrics(metrics.NullFactory),
	)
	if err != nil {
		panic(err)
	}
}

func main() {
	engine := gin.New()
	engine.GET("/", indexHandler)
	engine.GET("/home", homeHandler)
	engine.GET("/async", serviceHandler)
	engine.GET("/service", serviceHandler)
	engine.GET("/db", dbHandler)
	engine.Run(addr)
}

func dbHandler(c *gin.Context) {
	var sp opentracing.Span
	opName := c.Request.URL.Path
	wireContext, err := opentracing.GlobalTracer().Extract(
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(c.Request.Header))
	if err != nil {
		// 获取失败，则直接新建一个根节点 span
		sp = opentracing.StartSpan(opName)
	} else {
		sp = opentracing.StartSpan(opName, opentracing.ChildOf(wireContext))
	}
	defer sp.Finish()

	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
}

func serviceHandler(c *gin.Context) {

	// 通过http header，提取span元数据信息
	var sp opentracing.Span
	opName := c.Request.URL.Path
	wireContext, err := opentracing.GlobalTracer().Extract(
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(c.Request.Header))
	if err != nil {
		// 获取失败，则直接新建一个根节点 span
		sp = opentracing.StartSpan(opName)
	} else {
		sp = opentracing.StartSpan(opName, opentracing.ChildOf(wireContext))
	}
	defer sp.Finish()

	dbReq, _ := http.NewRequest("GET", "http://localhost:8080/db", nil)
	err = sp.Tracer().Inject(sp.Context(),
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(dbReq.Header))
	if err != nil {
		log.Fatalf("[dbReq]无法添加span context到http header: %v", err)
	}
	if _, err = http.DefaultClient.Do(dbReq); err != nil {
		sp.SetTag("error", true)
		sp.LogKV("请求 /db error", err)
	}

	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
}

func homeHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")

	c.String(200, "开始请求...\n")

	// 设置一个根节点 span
	span := opentracing.StartSpan("请求 /home")
	defer span.Finish()

	asyncReq, _ := http.NewRequest("GET", "http://localhost:8080/async", nil)
	err := span.Tracer().Inject(span.Context(),
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(asyncReq.Header))
	if err != nil {
		log.Fatalf("[asyncReq]无法添加span context到http header: %v", err)
	}
	go func() {
		if _, err := http.DefaultClient.Do(asyncReq); err != nil {
			span.SetTag("error", true)
			span.LogKV(fmt.Sprintf("请求 /async error: %v", err))
		}
	}()

	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)

	syncReq, _ := http.NewRequest("GET", "http://localhost:8080/service", nil)
	err = span.Tracer().Inject(span.Context(),
		opentracing.TextMap,
		opentracing.HTTPHeadersCarrier(syncReq.Header))
	if err != nil {
		log.Fatalf("[syncReq]无法添加span context到http header: %v", err)
	}
	if _, err = http.DefaultClient.Do(syncReq); err != nil {
		span.SetTag("error", true)
		span.LogKV(fmt.Sprintf("请求 /service error: %v", err))
	}
	c.String(200, "请求结束！")
}

func indexHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(200, string(`<a href="/home"> 点击开始发起请求 </a>`))
}
