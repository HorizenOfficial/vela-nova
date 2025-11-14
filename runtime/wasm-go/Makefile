APP := payment_app
OUT := build/$(APP).wasm
PROD_OUT := production_build/$(APP).wasm

.PHONY: all build production_build clean

build: $(OUT)

production_build: $(PROD_OUT)

all: build production_build

clean:
	rm -rf build production_build

$(OUT):
	@mkdir -p $(@D)
	@echo "Building development WASM module..."
	tinygo build -o $@ -target=wasi .

$(PROD_OUT):
	@mkdir -p $(@D)
	@echo "Building production WASM module..."
	tinygo build -o $@ -opt=2 -target=wasi .