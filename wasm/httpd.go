//go:build !js && !wasm

package main

// super-simple debug server to test our Go WASM files
// func main() {
// 	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
// 		if req.RequestURI == "/" {
// 			req.RequestURI = "/index.html"
// 		}
// 		file, err := os.Open(filepath.Join("./site", req.RequestURI))
// 		if err != nil {
// 			log.Fatal(err)
// 		}
// 		if _, err := io.Copy(w, file); err != nil {
// 			log.Fatal(err)
// 		}
// 	})
// 	log.Println("WARM testing server started on port 8080")
// 	log.Println(http.ListenAndServe(":8080", nil)) //nolint:gosec
// }
