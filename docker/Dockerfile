FROM alpine:3.17

RUN apk add --no-cache bash gcc g++ musl-dev gnu-libiconv cmake make git go

WORKDIR /opt/gnfd/workdir/

COPY ./iwasm .


RUN mkdir -p ./input
RUN mkdir -p ./output

ENTRYPOINT ./iwasm --max-gas=${MAX_GAS} ${WASM_FILE} ${INPUT_FILES} ${OUTPUT_FILES} 
