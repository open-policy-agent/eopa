package main

import (
	"fmt"
	"net/http"
)

const html = `<!DOCTYPE html>
<html>
	<head>
		<meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
		<script type="text/javascript">
function onLoad() {
  const urlParams = new URLSearchParams(window.location.search);
  const port = urlParams.get('callback')
  console.log(document.cookie.split("; "));
  
  fetch("http://127.0.0.1:"+port, {
    credentials: "include",
  });
}
window.onload = onLoad;
		</script>
	</head>
	<body>
		<h1>This incident will be reported.</h1>
	</body>
</html>`

func main() {
	http.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := http.Cookie{
			Name:     "gossid",
			Value:    "sesame",
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		}
		http.SetCookie(w, &c)
		fmt.Fprintln(w, html)
	}))

	panic(http.ListenAndServe("127.0.0.1:8080", nil))
}
