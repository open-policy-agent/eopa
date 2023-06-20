package disco

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
} {
	data.test.foo
}