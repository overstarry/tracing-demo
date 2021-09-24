package main

import (
	"context"
	"github.com/gin-contrib/logger"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/jaeger"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var (
	addr = ":8080"
)

// tracerProvider returns an OpenTelemetry TracerProvider configured to use
// the Jaeger exporter that will send spans to the provided url. The returned
// TracerProvider will also use a Resource configured with all the information
// about the application.
func tracerProvider(url string) (*tracesdk.TracerProvider, error) {
	// Create the Jaeger exporter
	exp, err := jaeger.New(jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(url)))
	if err != nil {
		return nil, err
	}
	tp := tracesdk.NewTracerProvider(
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
		tracesdk.WithBatcher(exp),
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("opentelemetry-overstarry"), // 服务名
			semconv.ServiceVersionKey.String("0.0.1"),
			attribute.String("environment", "test"),
		)),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp, nil
}

func main() {
	tp, err := tracerProvider("http://localhost:14268/api/traces")
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cleanly shutdown and flush telemetry when the application exits.
	defer func(ctx context.Context) {
		// Do not make the application hang when it is shutdown.
		ctx, cancel = context.WithTimeout(ctx, time.Second*5)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}(ctx)

	engine := gin.New()

	engine.Use(logger.SetLogger())
	engine.Use(otelgin.Middleware("server"))
	engine.GET("/", indexHandler)
	engine.GET("/home", homeHandler)
	engine.GET("/async", serviceHandler)
	engine.GET("/service", serviceHandler)
	engine.GET("/db", dbHandler)
	err = engine.Run(addr)
	if err != nil {
		return
	}
}

func dbHandler(c *gin.Context) {
	ctx := c.Request.Context()
	span := trace.SpanFromContext(otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(c.Request.Header)))
	defer span.End()

	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
}

func serviceHandler(c *gin.Context) {
	ctx := c.Request.Context()
	// 通过http header，提取span元数据信息
	span := trace.SpanFromContext(otel.GetTextMapPropagator().Extract(ctx, propagation.HeaderCarrier(c.Request.Header)))
	defer span.End()

	dbReq, _ := http.NewRequest("GET", "http://localhost:8080/db", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(dbReq.Header))
	if _, err := http.DefaultClient.Do(dbReq); err != nil {
		span.RecordError(err)
		attribute.String("请求 /db error", err.Error())
	}
	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)
}

func homeHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")

	c.String(200, "开始请求...\n")
	ctx := c.Request.Context()
	// 设置一个根节点 span
	span := trace.SpanFromContext(ctx)
	defer span.End()

	asyncReq, _ := http.NewRequest("GET", "http://localhost:8080/async", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(asyncReq.Header))
	go func() {
		if _, err := http.DefaultClient.Do(asyncReq); err != nil {
			span.RecordError(err)
			span.SetAttributes(attribute.String("请求 /async error", err.Error()))
		}
	}()

	time.Sleep(time.Duration(rand.Intn(200)) * time.Millisecond)

	syncReq, _ := http.NewRequest("GET", "http://localhost:8080/service", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(syncReq.Header))

	if _, err := http.DefaultClient.Do(syncReq); err != nil {
		span.RecordError(err)
		span.SetAttributes(attribute.String("请求 /service error", err.Error()))
	}
	c.String(200, "请求结束！")
}

func indexHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(200, string(`<a href="/home"> 点击发起请求 </a>`))
}
