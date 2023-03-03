# Data Plugins

The Data plugin API provides a service mechanism for reading and writing JSON data.
This data can be imported and used to make policy decisions.

## Running a data plugin

```shell
load --config-file http.yaml run -s -l debug
```
```yaml
# http.yaml: http plugin configuration file
plugins:
   data:
     foo.bar: # this is the path to the data once loaded
       type: http # required 
       
       polling_interval: 10s
       
       url: http://example.com/data.json
       method: GET
       body: "" # invalid for GET requests
       headers:
         Authorization: Bearer <token>
       timeout: 1s
       
       tls_skip_verification: true
       tls_client_cert: "cert.pem"
       tls_ca_cert: "ca.pem"
       tls_client_private_key: "key.pem"
       
       follow_redirects: false
```
