# Roadmap Detallado — kit-microsaas

Tareas concretas, archivos a modificar, estimación de líneas/complejidad.

---

## Fase 1 — Seguridad crítica

### 1. Hashear download tokens

**Archivos:**
- `security/token.go` — `HashToken()` ya existe pero no se usa
- `docgen/handler.go` — en `Upload` y donde se cree el download, llamar `HashToken(token)` y guardar `token_hash`
- `cleanup/cleanup.go:48` — cambiar de `token` a `id` para paths de archivos
- `db/migrations/k003_downloads.sql` — ya hay k006 que agrega `token_hash`

**Qué hacer:**
1. En `docgen/handler.go`, después de generar el token con `security.GenerateToken()`, calcular `hash := security.HashToken(token)` y guardar `token_hash` en el INSERT.
2. En `cleanup/cleanup.go`, dejar de buscar downloads por `token` para borrar archivos; usar `d.id` como subdirectorio.
3. Verificar que el handler de descarga (`/download/{token}`) busque por `token_hash` en vez de `token` plano.

**Estimación:** ~20 líneas, 1 archivo nuevo (ninguno, todo existe).

### 2. Arreglar `ratelimit.Check()` — Commit errors

**Archivos:** `ratelimit/ratelimit.go:28,40`

**Qué hacer:**
```go
// Reemplazar:
return tx.Commit() == nil, nil
// Por:
if err := tx.Commit(); err != nil {
    return false, fmt.Errorf("ratelimit commit: %w", err)
}
return true, nil
```

**Estimación:** 4 líneas cambiadas.

### 3. Transacción atómica payment+download en webhook

**Archivos:** `docgen/handler.go` — función `Webhook` (y donde se confirme pago)

**Qué hacer:**
1. Envolver en una tx:
```go
tx, err := db.Conn.Begin()
if err != nil { ... }
defer tx.Rollback()

_, err = tx.Exec("UPDATE payments SET status = 'confirmed', confirmed_at = datetime('now') WHERE id = ? AND status = 'pending'", paymentID)
_, err = tx.Exec("UPDATE downloads SET status = 'paid', paid_at = datetime('now') WHERE id = ?", downloadID)

return tx.Commit()
```

**Estimación:** ~15 líneas.

### 4. Arreglar `isRequestSecure()` para proxy

**Archivos:** `csrf/csrf.go:45-56`

**Qué hacer:**
- Quitar la restricción `strings.HasPrefix(host, "127.0.0.1:")`.
- Pasar `cfg.IsProduction()` a `setCookie()` y usarlo directamente para `Secure`.
- Si `X-Forwarded-Proto: https` está presente, confiar sin importar RemoteAddr.

**Estimación:** ~10 líneas.

### 5. Cleanup race condition

**Archivos:** `cleanup/cleanup.go:30-52`

**Qué hacer:**
- Cambiar `DELETE` por `UPDATE status = 'expired' WHERE status = 'pending' AND ...`
- Verificar `RowsAffected` antes de borrar archivos.
- No DELETE archivos si el download se pagó entre la SELECT y el UPDATE.

**Estimación:** ~15 líneas.

### 6. Tailwind estático + CSP sin `unsafe-inline`

**Archivos:**
- `views/layout.html` — cambiar CDN por `<link rel="stylesheet" href="/static/tailwind.css">`
- `middleware/security.go` — remover `unsafe-inline` de script-src y style-src
- Nuevo: `scripts/build-tailwind.sh` (o instrucción manual)
- `cmd/main.go` — agregar file server para `/static/`

**Qué hacer:**
1. Ejecutar `npx tailwindcss -i ./views/input.css -o ./static/tailwind.css --minify`
2. Servir `./static` como `/static/` en el mux
3. Actualizar CSP

**Estimación:** ~30 líneas + setup de Tailwind CLI.

### 7. Cleanup atómico

**Archivos:** `cleanup/cleanup.go:61-68`

**Qué hacer:**
```go
tx, err := db.Begin()
if err != nil { ... }
defer tx.Rollback()
tx.Exec("DELETE FROM payments WHERE download_id = ?", id)
tx.Exec("DELETE FROM downloads WHERE id = ?", id)
tx.Commit()
```

**Estimación:** ~10 líneas.

### 8. Log CSRF token error

**Archivos:** `csrf/csrf.go:85-89`

**Qué hacer:**
```go
if err != nil {
    slog.Error("csrf generate token", "error", err)
    http.Error(w, "Error interno", http.StatusInternalServerError)
    return
}
```

**Estimación:** 4 líneas.

### 9. TTL en idempotency_keys

**Archivos:**
- `db/migrations/k007_idempotency.sql` — ya existe `expires_at`
- `cleanup/cleanup.go` — agregar DELETE de idempotency_keys expiradas

**Estimación:** ~5 líneas.

### 10. Limitar CORS methods

**Archivos:** `middleware/security.go:59-60`

**Qué hacer:** Ya está corregido a `GET, POST, OPTIONS` (de la versión anterior que tenía PUT/DELETE).

**Estimación:** 0 líneas (ya hecho).

---

## Fase 1b — Free trial + Planes (completada)

### 11. Conectar `ratelimit.Check()` en Upload

**Archivos:** `docgen/handler.go:Upload`

**Qué hacer:**
1. Llamar `ratelimit.Check(ctx, db.Conn, "upload:"+ip, 10, time.Hour)` al inicio de Upload
2. Si blocked, responder con 429 y mensaje "Demasiadas solicitudes"
3. Setear cookie `device_id` si no existe

**Archivos modificados:** 1
**Líneas:** ~15

### 12. Migración `k010_trial_pass.sql`

**Archivos:**
- `db/migrations/k010_trial_pass.sql` — nueva migración
- `db/db.go` — agregar a `kitMigrations`

**Qué hacer:**
```sql
CREATE TABLE trial_tracking (
    key         TEXT PRIMARY KEY,
    doc_count   INTEGER NOT NULL DEFAULT 0,
    window_start TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at  TEXT
);
```

**Archivos modificados:** 2
**Líneas:** ~15

### 13. Free trial: `getTrialInfo()` + `recordTrialUsage()`

**Archivos:** `docgen/handler.go`

**Qué hacer:**
1. `getTrialInfo(ctx, ip string)`:
   - SELECT de `trial_tracking` WHERE `key = 'trial:' + ip`
   - Si no existe: returns `{remaining: 30, windowStart: now}`
   - Si `window_start + 30 days < now`: reset (UPDATE window_start = now, doc_count = 0)
   - returns `{remaining: 30 - doc_count, canUseFree: remaining > 0}`
2. `recordTrialUsage(ctx, ip string, count int)`:
   - INSERT ON CONFLICT(key) DO UPDATE SET doc_count = doc_count + count

**Archivos modificados:** 1
**Líneas:** ~40

### 14. Plan Batch ($2.990) + Plan Pase ($6.990)

**Archivos:** `docgen/handler.go` (Pay, generateAndServe, createFlowPayment, Webhook)

**Qué hacer:**
1. Pay handler lee `plan` de `r.FormValue("plan")`:
   - `"free"`: check trial → generate → recordTrialUsage
   - `"batch"`: generate → insert payment (price_clp from DB)
   - `"pass"`: generate → insert payment (6990) → recordPass (whitelist IP 30 days)
2. `generateAndServe(amount)` — acepta amount como parámetro en vez de hardcode 2990
3. `createFlowPayment(amount, subject)` — acepta amount + subject
4. Webhook: si `amount >= 6990`, llama `recordPass(ip)`

**Archivos modificados:** 1
**Líneas:** ~100

### 15. Pricing UI con 3 estados

**Archivos:** `views/tools/docgen-show.html`

**Qué hacer:**
1. Estado PAS (pass activo): banner verde "Tienes pase activo hasta [fecha]", botón "Generar documentos"
2. Estado TRIAL (trial disponible): banner azul con docs restantes + botones "Gratis (X docs)", "Batch $2.990", "Pase $6.990"
3. Estado AGOTADO (trial vencido): banner rojo "Trial agotado", solo botones "Batch $2.990" y "Pase $6.990"
4. Precios renderizados con `formatCLP`

**Archivos modificados:** 1
**Líneas:** ~50

### 16. Límite 300 filas

**Archivos:** `docgen/handler.go` — DataUpload

**Qué hacer:** Cambiar de 1000 a 300 el máximo de filas de Excel.

**Archivos modificados:** 1
**Líneas:** 1

### 17. Eliminar `k009_docgen_session.sql` leftover

**Archivos:** `db/migrations/k009_docgen_session.sql` — eliminar archivo

**Razón:** Era un ALTER TABLE de columnas que ya existen en k008. Causaría error en DB nueva.

**Archivos modificados:** 1 eliminado

---

## Fase 1c — Segunda ronda auditoría (completada 30 jun 2026)

### C1. generateAndServe retorna error

**Archivos:** `docgen/handler.go`

**Qué hacer:**
- `generateAndServe()` ya no traga errores: todas las fallas (lectura template, ZIP, exec SQL) se retornan como `error`
- El caller (Pay, Webhook) verifica el error y responde con 500 + log
- `log/slog` importado en handler.go

**Archivos modificados:** 1
**Líneas:** ~30

### C3. Webhook reordenado

**Archivos:** `docgen/handler.go`

**Qué hacer:**
- Webhook ejecuta `generateAndServe()` primero (que genera ZIP + INSERT payment + UPDATE download en una tx)
- Solo después registra el pass si `amount >= 6990`
- Se eliminó la transacción separada para `flow_token` (integrada en generateAndServe)

**Archivos modificados:** 1
**Líneas:** ~15

### H1. DataUpload verifica error de UPDATE

**Archivos:** `docgen/handler.go:DataUpload`

**Qué hacer:**
- Verificar el error retornado por `ExecContext` después del UPDATE downloads SET data_rows
- Si falla, responder 500

**Archivos modificados:** 1
**Líneas:** 4

### H2. Show verifica batchPrice query error

**Archivos:** `docgen/handler.go:Show`

**Qué hacer:**
- Cambiar `QueryRowContext(...).Scan(&batchPrice); if batchPrice == 0` por `if err := ...Scan(&batchPrice); err != nil { batchPrice = 2990 }`

**Archivos modificados:** 1
**Líneas:** 2

### H3. recordTrialUsage antes de generateAndServe

**Archivos:** `docgen/handler.go:Pay` (free case)

**Qué hacer:**
- Llamar `recordTrialUsage()` antes de `generateAndServe()` para asegurar que si el conteo falla, el ZIP no se genera sin descuento

**Archivos modificados:** 1
**Líneas:** 2

### H4. Idempotencia en Pay

**Archivos:** `docgen/handler.go:Pay`

**Qué hacer:**
- Al inicio de Pay, antes del switch de plan, leer `r.FormValue("idempotency_key")`
- Consultar `idempotency_keys` con key `pay:{idk}`
- Si existe la key, responder 409 "Solicitud duplicada"
- Si no existe, proceder con el pago

**Archivos modificados:** 1
**Líneas:** ~15

### M2. Status endpoint 404

**Archivos:** `docgen/handler.go:Status`

**Qué hacer:**
- Si el token no existe en DB, responder `http.Error(w, "No encontrado", 404)` en vez de renderizar template incorrecto

**Archivos modificados:** 1
**Líneas:** 2

### M3. Rate limit en DataUpload y Pay

**Archivos:** `docgen/handler.go`

**Qué hacer:**
- DataUpload: `ratelimit.Check(ctx, db, "data:"+ip, 20, 1h)`
- Pay: `ratelimit.Check(ctx, db, "pay:"+ip, 10, 1h)`
- Ambos retornan 429 si se excede el límite

**Archivos modificados:** 1
**Líneas:** ~16

### B1. No exponer paths en Upload error

**Archivos:** `docgen/handler.go:Upload`

**Qué hacer:**
- Cambiar `"Error al leer plantilla: "+err.Error()` por mensaje genérico "Plantilla invalida o sin marcadores {{...}}"

**Archivos modificados:** 1
**Líneas:** 1

### B2. CSP remover data: de img-src

**Archivos:** `middleware/security.go`

**Qué hacer:**
- CSP: `img-src 'self'` (sin `data:`)

**Archivos modificados:** 1
**Líneas:** 1

### Extra: Plan inválido retorna 400

**Archivos:** `docgen/handler.go:Pay`

**Qué hacer:**
- En el switch de plan, agregar `default: http.Error(w, "Plan invalido", 400)`

**Archivos modificados:** 1
**Líneas:** 2

### Extra: Excel sheet name dinámico

**Archivos:** `docgen/handler.go:DataUpload`

**Qué hacer:**
- Cambiar `f.GetRows("Sheet1")` por `f.GetRows(f.GetSheetList()[0])`

**Archivos modificados:** 1
**Líneas:** 1

### Extra: flow_token en generateAndServe

**Archivos:** `docgen/handler.go`

**Qué hacer:**
- `generateAndServe()` acepta `flowToken string` y lo almacena en `payments.flow_token`

**Archivos modificados:** 1
**Líneas:** 3

---

## Fase 2 — Flow.cl real

### 11. Handler creación de pago Flow.cl

**Archivos:** `docgen/handler.go` — función `Pay`

**Qué hacer:**
1. En producción (`!cfg.IsDevelopment()`), llamar a la API de Flow.cl `POST /api/payment/create`
2. Body: `apiKey`, `commerceId`, `subject`, `amount`, `urlReturn`, `urlConfirmation`, `optional` (download_id)
3. Firma HMAC-SHA256 con `FlowSecretKey`
4. Redirigir a `flowUrl` devuelto por Flow.cl

**Estimación:** ~50 líneas. Alta dependencia de la API real de Flow.cl.

### 12. Webhook real

**Archivos:** `docgen/handler.go` — función `Webhook`

**Qué hacer:**
1. Validar HMAC del body con `VerifyHMAC()`
2. Extraer `flowToken`, `paymentId`
3. Buscar payment por `flow_token`
4. Actualizar payment + download en transacción (punto 3)
5. Llamar `generateAndServe()` para preparar ZIP

**Estimación:** ~40 líneas.

### 13. Endpoint de polling /status/{token}

**Archivos:** `docgen/handler.go` — función `Status`

**Qué hacer:**
- Ya existe el endpoint `GET /status/{token}`
- Verificar que consulta payments.status + downloads.status y muestra estado al usuario
- Si está pending y pasaron >5 min, mostrar "estamos verificando tu pago"

**Estimación:** ~10 líneas (refinar la view).

### 14. Soft-delete de tokens

**Archivos:** `docgen/handler.go` — función `Download`

**Qué hacer:**
1. Marcar token como `downloading` con timestamp
2. Servir archivo
3. Marcar como `paid` después de `io.Copy` exitoso
4. Si está `downloading` por >5 min, permitir reintento

**Estimación:** ~25 líneas.

---

## Fase 3 — Monitoreo

### 15. Health check

**Archivos:** `cmd/main.go`

**Qué hacer:**
```go
mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
    err := db.Conn.Ping()
    if err != nil {
        w.WriteHeader(http.StatusServiceUnavailable)
        json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy"})
        return
    }
    w.WriteHeader(http.StatusOK)
    json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
})
```

**Estimación:** ~15 líneas.

### 16. Logs a archivo

**Archivos:** `cmd/main.go` — `slog.SetDefault`

**Qué hacer:**
- En producción: escribir a `stdout` (manejado por systemd/supervisor)
- Opcional: rotación diaria con `lumberjack` o similar

**Estimación:** ~5 líneas.

### 17. Métricas básicas

**Archivos:** `middleware/security.go` o nuevo `middleware/metrics.go`

**Qué hacer:**
- Contadores en memoria: requests totales, por ruta, 5xx, payments iniciados, payments confirmados
- Endpoint `GET /metrics` (protegido por token fijo)

**Estimación:** ~40 líneas.

### 18-19. Alertas y dashboard

**Archivos:** Nuevo `monitor/` package

**Qué hacer:**
- Goroutine que revisa payments en estado `pending` con >5 min de antigüedad
- Loggear warning (o enviar notificación si hay sistema)

**Estimación:** ~30 líneas.

---

## Fase 4 — Deploy

### 20. Dockerfile

**Archivo nuevo:** `Dockerfile`

**Qué hacer:**
```dockerfile
FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server ./cmd

FROM alpine:3.19
WORKDIR /app
COPY --from=build /app/server .
COPY views/ ./views/
COPY db/migrations/ ./db/migrations/
EXPOSE 8090
CMD ["./server"]
```

**Consideraciones:** SQLite necesita `CGO_ENABLED=1` o usar `modernc.org/sqlite` (pure Go). Si se usa `mattn/go-sqlite3` (CGO), el build cambia.

**Estimación:** ~25 líneas.

### 21. Reverse proxy

**Archivo nuevo:** `Caddyfile` o `nginx.conf`

**Caddy (recomendado):**
```
docgen.kit.app {
    reverse_proxy localhost:8090
}
```

**Nginx:**
```nginx
server {
    listen 443 ssl;
    server_name docgen.kit.app;
    location / {
        proxy_pass http://localhost:8090;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

### 22-23. Dominio y env vars

**Checklist:**
- [ ] Comprar dominio (ej: `kitdocgen.cl`)
- [ ] Apuntar DNS al VPS
- [ ] Configurar TLS (Caddy lo hace automático)
- [ ] Setear env vars: `ENV=production`, `PORT=:8090`, `FLOW_API_KEY`, `FLOW_SECRET_KEY`, `ALLOWED_ORIGINS=https://docgen.kit.app`

### 24. Backup de SQLite

**Script nuevo:** `scripts/backup.sh`

```bash
#!/bin/sh
cp /app/data/app.db /app/backups/app-$(date +%Y%m%d-%H%M%S).db
find /app/backups -name "*.db" -mtime +30 -delete
```

**Cron:** `0 3 * * * /app/scripts/backup.sh`

### 25. CI/CD

**Archivo nuevo:** `.github/workflows/deploy.yml`

**Qué hacer:**
- On push a main: build Go, run vet, run tests (cuando existan), scp binario al VPS, restart servicio

---

## Fase 5 — Escalabilidad

### 26. Context en queries SQL

**Archivos:** Todos los `.go` que usan `db.Conn.QueryRow`, `db.Conn.Exec`, `db.Conn.Begin`

**Qué hacer:** Migrar a variantes `Context` pasando `r.Context()` en handlers HTTP.

**Estimación:** ~30-50 líneas en total (muchos archivos, cambios pequeños).

### 27. Mutex en templates

**Archivos:** `template/template.go:78-81`

**Qué hacer:** Agregar `sync.RWMutex` para proteger `e.templates`.

**Estimación:** ~15 líneas.

### 28-31. Optimizaciones menores

- Cache de templates: ya existe cuando `reload=false` (producción)
- WAL mode: `PRAGMA journal_mode=WAL;` en migración k000
- Límite data_rows: validar tamaño en `docgen/handler.go:DataUpload`

---

## Resumen de estimación

| Fase | Archivos modificados | Líneas totales | Riesgo |
|------|---------------------|----------------|--------|
| F1 — Seguridad | ~8 | ~80 | Bajo (completada) |
| F1b — Trial + Planes | ~6 | ~220 | Medio (completada) |
| F1c — 2da ronda auditoría | ~3 | ~100 | Bajo (completada) |
| F2 — Flow.cl | ~2 | ~125 | Alto (depende de API externa) |
| F3 — Monitoreo | ~3 | ~90 | Bajo |
| F4 — Deploy | ~5 nuevos | ~60 | Medio (infraestructura) |
| F5 — Escalabilidad | ~10 | ~100 | Bajo |

**Total estimado:** ~775 líneas nuevas/modificadas + archivos de infraestructura.
