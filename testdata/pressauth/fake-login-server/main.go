// fake-login-server runs the testdata/pressauth/fakelogin handler on an
// ephemeral 127.0.0.1 port and prints its base URL on stdout. press-auth
// U3 smoke runs start this binary, read the URL, then drive the chromedp
// launcher against it.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/mvanhorn/cli-printing-press/v4/testdata/pressauth/fakelogin"
)

func main() {
	port := flag.Int("port", 0, "TCP port to listen on (0 = pick an ephemeral port)")
	domain := flag.String("cookie-domain", fakelogin.DefaultCookieDomain, "Domain attribute stamped on the session cookie (empty means host-only)")
	flag.Parse()

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	base := fmt.Sprintf("http://%s", lis.Addr().String())
	fmt.Printf("Listening on %s\n", base)

	srv := &http.Server{Handler: fakelogin.NewHandler(*domain)}

	done := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		_ = srv.Close()
		close(done)
	}()

	if err := srv.Serve(lis); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
	<-done
}
