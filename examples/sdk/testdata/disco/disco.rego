package disco
import rego.v1

config := {
 	"services": [
		{
			"name": "bndl",
			"url": opa.runtime().env.BUNDLE_HOST,
		}
	],
	"bundles": {
		"bundle.bjson.tar.gz": {
			"service": "bndl"
		}
	}
} if data.test.foo
