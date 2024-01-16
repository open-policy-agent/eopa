
.PHONY: build irdump irviz iropt irrun clean
build: irdump irviz iropt irrun

irdump:
	go build -o irdump cmd/irdump/main.go

irviz:
	go build -o irviz cmd/irviz/*.go

iropt:
	go build -o iropt cmd/iropt/main.go

irrun:
	go build -o irrun cmd/irrun/main.go

clean:
	rm -f irdump irviz iropt irrun
