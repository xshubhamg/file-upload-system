.PHONY: run build vet tidy test clean docker-build docker-run

APP := server

run:
	go run .

build:
	go build -o $(APP) .

vet:
	go vet ./...

tidy:
	go mod tidy

test:
	go test ./...

clean:
	rm -f $(APP) db.sqlite
	rm -rf uploads/

docker-build:
	docker build -t file-upload .

docker-run:
	docker run --rm -p 8080:8080 --env-file .env -v $(PWD)/uploads:/app/uploads file-upload
