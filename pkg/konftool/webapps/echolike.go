package webapps

import "github.com/labstack/echo/v4"

// Common interface for Echo-like objects that allow route registration.
// This allows passing eithere echo.Echo or echo.Group. It also limits
// the kinds of routes we can set to the ones we actually use
type EchoLike interface {
	GET(path string, h echo.HandlerFunc, middleware ...echo.MiddlewareFunc) *echo.Route
	POST(path string, h echo.HandlerFunc, middleware ...echo.MiddlewareFunc) *echo.Route
}