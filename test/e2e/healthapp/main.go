// Command healthapp is a test app for the health check e2e tests. It serves
// /healthz on 127.0.0.1:$PORT and answers 200, except while the file
// $HANG_FILE exists: then it hangs without answering, like a deadlocked app
// that has not exited.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	hang := os.Getenv("HANG_FILE")
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		for hang != "" {
			if _, err := os.Stat(hang); err != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		fmt.Fprintln(w, "ok")
	})
	fmt.Printf("healthapp %d serving on 127.0.0.1:%s\n", os.Getpid(), os.Getenv("PORT"))
	if err := http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), nil); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
