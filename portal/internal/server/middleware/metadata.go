package middleware

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"google.golang.org/grpc/metadata"
)

func parseHost(request *http.Request) metadata.MD {
	var pairs []string
	log.Infof("request.Host : %s", request.Host)
	log.Infof("Headers: %s", request.Header)
	tcpAddr := request.Context().Value(http.LocalAddrContextKey).(*net.TCPAddr)
	pairs = append(pairs, "origin-host", tcpAddr.IP.String())
	pairs = append(pairs, "x-vmid", request.Header.Get("x-vmid"))
	return metadata.Pairs(pairs...)
}

func MetaData() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (reply interface{}, err error) {
			request, ok := khttp.RequestFromServerContext(ctx)
			if !ok {
				return nil, fmt.Errorf("context can't find http Request")
			}
			ctx = metadata.NewIncomingContext(ctx, parseHost(request))
			return handler(ctx, req)
		}
	}
}
