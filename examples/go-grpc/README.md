# Golang gRPC Client Demo

## Build the `grpcpush` demo program

You can install dependencies and build the demo program with the following `make` commands:

```
make install
make grpc-gen-go  # Runs `buf generate` in the top-level `proto/` folder.
make build
```

## Run the demo

In one terminal, start up a Styra Enterprise OPA instance with the provided config file:

```bash
$ ./eopa -c config.yaml
```

In a second terminal, let's fire up the gRPC client program:

```bash
$ export ADDR=':9090'
$ ./grpcpush
```

Output will continue until you hit `Ctrl-C` to stop the program.


## Check the output

If you're on a Linux-like system, you can expect to see output similar to the following (numbers may be different depending on system configuration, installed RAM, etc.):

```
/example/vmem_summary
k:          active, 	v: 9.177375793457031 Gb
k:       available, 	v: 21.976478576660156 Gb
k:         buffers, 	v: 2.6309547424316406 Gb
k:          cached, 	v: 17.35655975341797 Gb
k:     commitLimit, 	v: 16.570178985595703 Gb
k:     committedAS, 	v: 19.893768310546875 Gb
k:           dirty, 	v: 4.47265625 Mb
k:            free, 	v: 3.9379234313964844 Gb
k:        highFree, 	v: 0 b
k:       highTotal, 	v: 0 b
k:    hugePageSize, 	v: 2 Mb
k:   hugePagesFree, 	v: 0 b
k:   hugePagesRsvd, 	v: 0 b
k:   hugePagesSurp, 	v: 0 b
k:  hugePagesTotal, 	v: 0 b
k:        inactive, 	v: 14.604598999023438 Gb
k:         laundry, 	v: 0 b
k:         lowFree, 	v: 0 b
k:        lowTotal, 	v: 0 b
k:          mapped, 	v: 1.180999755859375 Gb
k:      pageTables, 	v: 73.94921875 Mb
k:          shared, 	v: 1.5105247497558594 Gb
k:            slab, 	v: 1.8739776611328125 Gb
k:    sreclaimable, 	v: 1.5899429321289062 Gb
k:      sunreclaim, 	v: 290.8515625 Mb
k:      swapCached, 	v: 0 b
k:        swapFree, 	v: 975.99609375 Mb
k:       swapTotal, 	v: 975.99609375 Mb
k:           total, 	v: 31.234119415283203 Gb
k:            used, 	v: 7.308681488037109 Gb
k:     usedPercent, 	v: 23.399672.3
k:    vmallocChunk, 	v: 0 b
k:    vmallocTotal, 	v: 32767.999999046326 Gb
k:     vmallocUsed, 	v: 64.75390625 Mb
k:           wired, 	v: 0 b
k:       writeBack, 	v: 0 b
k:    writeBackTmp, 	v: 0 b
/example/vmem_summary
k:          active, 	v: 9.177375793457031 Gb
k:       available, 	v: 22.005672454833984 Gb
k:         buffers, 	v: 2.6309547424316406 Gb
k:          cached, 	v: 17.337547302246094 Gb
k:     commitLimit, 	v: 16.570178985595703 Gb
k:     committedAS, 	v: 19.86498260498047 Gb
k:           dirty, 	v: 4.47265625 Mb
k:            free, 	v: 3.9671173095703125 Gb
k:        highFree, 	v: 0 b
k:       highTotal, 	v: 0 b
k:    hugePageSize, 	v: 2 Mb
k:   hugePagesFree, 	v: 0 b
k:   hugePagesRsvd, 	v: 0 b
k:   hugePagesSurp, 	v: 0 b
k:  hugePagesTotal, 	v: 0 b
k:        inactive, 	v: 14.591140747070312 Gb
k:         laundry, 	v: 0 b
k:         lowFree, 	v: 0 b
k:        lowTotal, 	v: 0 b
k:          mapped, 	v: 1.1811141967773438 Gb
k:      pageTables, 	v: 73.80078125 Mb
k:          shared, 	v: 1.4915122985839844 Gb
k:            slab, 	v: 1.8739776611328125 Gb
k:    sreclaimable, 	v: 1.5899429321289062 Gb
k:      sunreclaim, 	v: 290.8515625 Mb
k:      swapCached, 	v: 0 b
k:        swapFree, 	v: 975.99609375 Mb
k:       swapTotal, 	v: 975.99609375 Mb
k:           total, 	v: 31.234119415283203 Gb
k:            used, 	v: 7.298500061035156 Gb
k:     usedPercent, 	v: 23.367075.3
k:    vmallocChunk, 	v: 0 b
k:    vmallocTotal, 	v: 32767.999999046326 Gb
k:     vmallocUsed, 	v: 64.75390625 Mb
k:           wired, 	v: 0 b
k:       writeBack, 	v: 0 b
k:    writeBackTmp, 	v: 0 b
...
```
