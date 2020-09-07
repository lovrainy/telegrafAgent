package api

import (
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/cors"
	"net/http"
)


// 跨域中间件
func CorsMiddleware() *cors.Cors {
	corsMiddleware := cors.New(cors.Options{
		// AllowedOrigins: []string{"https://foo.com"}, // Use this to allow specific origin hosts
		AllowedOrigins:   []string{"*"},
		// AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300, // Maximum value not ignored by any of major browsers
	})
	return corsMiddleware
}

func MainRouter() http.Handler {
	r := chi.NewRouter()

	// 加载中间件
	cors := CorsMiddleware()
	r.Use(cors.Handler) // 跨域中间件
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.DefaultCompress) // gzip压缩
	r.Use(middleware.StripSlashes)
	r.Use(middleware.Recoverer)  // 从panic中恢复崩溃的服务

	// 挂载视图
	r.Get("/file/list", FileListHandler)  // 登出接口为rest接口
	r.Post("/file/delete", FileDeleteHandler)

	return r
}

