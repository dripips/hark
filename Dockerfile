# Hark в контейнере.
#
# Собирать особо нечего: SQLite здесь на чистом Go, cgo не нужен, а шаблоны,
# админка и виджет зашиты в бинарник. Получается один статический файл.
#
# Итоговый слой — alpine, а не scratch, хотя один файл в пустом образе выглядел
# бы красивее. Причины две, и обе про чужую боль, а не про красоту: в scratch
# некуда положить временные файлы SQLite, и туда нельзя зайти оболочкой, когда
# что-то пошло не так. Восемь мегабайт того стоят.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Зависимости отдельным слоем: правка кода не заставляет качать их заново.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hark .

# ── Итоговый образ ──────────────────────────────────────────────────────

FROM alpine:3.22

# Сертификаты нужны, чтобы достучаться до поставщика модели по https.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 65532 hark \
 && mkdir -p /data && chown hark:hark /data

COPY --from=build /out/hark /usr/local/bin/hark

# База лежит в томе, а не в слое образа: иначе она исчезнет при обновлении.
# Каталог создан и отдан пользователю ДО объявления тома — иначе Docker создаст
# точку монтирования от рута, и Hark не запишет в неё ни байта.
VOLUME /data
ENV HARK_DB=/data/hark.db
ENV HARK_ADDR=:8080
EXPOSE 8080

USER hark
WORKDIR /data

ENTRYPOINT ["hark"]
