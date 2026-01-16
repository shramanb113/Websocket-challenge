package main

import (
	"fmt"
	"net/http"

	"github.com/a-h/templ"
	"websocket-challenge/views"
)

func main() {
	// We call views.Layout("YourName") to create the component
	// then templ.Handler() wraps it so http.ListenAndServe can use it.

	http.Handle("/", templ.Handler(views.Layout("Lead Developer")))

	fmt.Println("🚀 Server starting at http://localhost:8080")

	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Printf("Error starting server: %s\n", err)
	}
}
