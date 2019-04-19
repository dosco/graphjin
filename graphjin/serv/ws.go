package serv

/*
func apiv1Ws(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.Close()
	for {
		mt, message, err := c.ReadMessage()
		if err != nil {
			fmt.Println("read:", err)
			break
		}
		fmt.Printf("recv: %s", message)
		err = c.WriteMessage(mt, message)
		if err != nil {
			fmt.Println("write:", err)
			break
		}
	}
}

func serve(w http.ResponseWriter, r *http.Request) {
	// if websocket.IsWebSocketUpgrade(r) {
	// 	apiv1Ws(w, r)
	// 	return
	// }
	apiv1Http(w, r)
}
*/
