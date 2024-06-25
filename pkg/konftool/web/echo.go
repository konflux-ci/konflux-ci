package web

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// An opaque wrapper around http.Server for adding our Start/Stop logic
type Web struct{ server *http.Server }

// Handler
func hello(c echo.Context) error {
	return c.String(http.StatusOK, "Hello, World!")
}

func setupEcho() *echo.Echo {
	// Echo instance
	e := echo.New()

	// Middleware
	e.Use(middleware.Logger())
	e.Use(middleware.Recover())

	// Routes
	e.GET("/", hello)

	return e
}

// Starts the web server in the background or returns
// an error if it fails to start
func Start(listenAddr string) (*Web, error) {
	// Setup our own listener to ensure the port is free
	lsnr, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	// Using a custom server so we can stop it
	srv := http.Server{Handler: setupEcho()}
	go srv.Serve(lsnr)

	return &Web{server: &srv}, nil
}

func (w *Web) Stop() error {
	// Try to shut down up to 10 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return w.server.Shutdown(ctx)
}
