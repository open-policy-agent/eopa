
.PHONY: build irdump irviz iropt clean
build: irdump irviz iropt

irdump:
	go build -o irdump cmd/irdump/main.go

irviz:
	go build -o irviz cmd/irviz/*.go

iropt:
	go build -o iropt cmd/iropt/main.go

clean:
	rm -f irdump irviz iropt
