# Auditoría de kit-microsaas

**Auditor:** LLM Auditor | **Fecha:** 2026-06-25 (v3) | **Archivos auditados:** 11 `.go` + 9 `.sql` + `AGENTS.md`

---

## Resumen ejecutivo

El kit es sorprendentemente bueno para un proyecto generado por LLM: las decisiones arquitectónicas son coherentes, la superficie de ataque se redujo drásticamente al eliminar auth/email/roles, y los controles de seguridad básicos (CSRF, path traversal, parámetros preparados) están correctos.

**Estado de hallazgos anteriores:** 10 de 11 hallazgos de la auditoría original ya están corregidos (token hasheado ✅, ratelimit commits ✅, isRequestSecure ✅, cleanup race condition ✅, cleanup atómico ✅, CSRF error logging ✅, CORS methods ✅, CSP/unsafe-inline ✅, context.Context en queries ✅, template reload race condition ✅). Queda pendiente: integración real con Flow.cl.

---

## Hallazgos por dimensión

### Dimensión 1 — Seguridad

**🔴 CRÍTICO — Token de descarga almacenado en texto plano (contradice la documentación) — ✅ CORREGIDO**
- **Dónde:** `db/migrations/k006_token_hash.sql`, `docgen/handler.go:87`
- **Problema original:** AGENTS.md dice "Stored hashed (SHA-256)" pero la migración original k003 define `token TEXT UNIQUE NOT NULL` sin hash.
- **Fix:** Migración k006 agregó columna `token_hash`, handler `Upload` inserta `security.HashToken(token)`, handlers `Download`/`Status`/`Webhook` buscan por `token_hash`. El token original viaja solo en URL de redirect y nunca se persiste. La migración k008 agregó `token_hash` al INSERT de downloads.
- **Estado:** ✅ Corregido. k003 original guardaba texto plano, pero las migraciones posteriores y el código actual usan hash.

**🔴 CRÍTICO — `ratelimit.Check()` traga error de Commit (falso positivo) — ✅ CORREGIDO**
- **Dónde:** `ratelimit/ratelimit.go:28,43,61`
- **Problema original:** `return tx.Commit() == nil, nil` convertía error de Commit en falso positivo.
- **Fix:** Las líneas 28, 43, 61 ahora usan `if err := tx.Commit(); err != nil { return false, err }`.
- **Estado:** ✅ Corregido en código actual.

**🟠 ALTO — `isRequestSecure()` no funciona detrás de reverse proxy — ✅ CORREGIDO**
- **Dónde:** `csrf/csrf.go:45-55`
- **Problema original:** Función `isRequestSecure()` confiaba en `X-Forwarded-Proto` solo si RemoteAddr era localhost.
- **Fix:** `isRequestSecure()` fue eliminada. `setCookie()` ahora recibe `secure bool` directamente desde `cfg.IsProduction()`.
- **Estado:** ✅ Corregido en código actual.

**🟠 ALTO — Cleanup race condition: puede borrar un download recién pagado — ✅ CORREGIDO**
- **Dónde:** `cleanup/cleanup.go:56-86`
- **Problema original:** SELECT sin lock + DELETE podía borrar downloads pagados si el webhook llegaba durante cleanup.
- **Fix:** Se reemplazó DELETE por `UPDATE downloads SET status = 'expired' WHERE id = ? AND status IN ('draft', 'ready', 'pending')`. Se verifica `RowsAffected` antes de borrar archivos. Si el webhook cambió status a 'paid' entre la SELECT y el UPDATE, el UPDATE no afecta filas y no se borran archivos.
- **Estado:** ✅ Corregido. La query filtra por `d.status IN ('draft', 'ready', 'pending')` explícitamente.

**🟠 ALTO — Cleanup borra payments sin transacción atómica — ✅ CORREGIDO**
- **Dónde:** `cleanup/cleanup.go:56-78`
- **Problema original:** Statements separados sin transacción.
- **Fix:** `expireDownload()` ahora usa `db.Begin()` + `defer tx.Rollback()` + `tx.Commit()` para UPDATE + DELETE.
- **Estado:** ✅ Corregido.

**🟡 MEDIO — CSRF token silenciosamente falla si crypto/rand falla — ✅ CORREGIDO**
- **Dónde:** `csrf/csrf.go:77`
- **Problema original:** El error de `GenerateToken()` se ignoraba.
- **Fix:** `slog.Error("csrf generate token", "error", err)` agregado en línea 77.
- **Estado:** ✅ Corregido.

**🟡 MEDIO — CSP depende de CDN de terceros con `unsafe-inline` — ✅ CORREGIDO**
- **Dónde:** `middleware/security.go:10`, `static/tailwind.css`
- **Problema original:** Tailwind CDN requería `'unsafe-inline'` para scripts y estilos.
- **Fix:** Tailwind compilado a `static/tailwind.css` (v4, minified, ~4KB), servido desde `'self'`. CSP ahora es: `default-src 'self'; script-src 'self' https://unpkg.com; style-src 'self'; ...`. Sin `unsafe-inline`, sin CDN de Tailwind.
- **Estado:** ✅ Corregido.

**🔵 BAJO — CORS expone métodos PUT/DELETE — ✅ CORREGIDO**
- **Dónde:** `middleware/security.go:60`
- **Problema original:** `DefaultCORSConfig()` incluía PUT y DELETE.
- **Fix:** Limitado a `GET, POST, OPTIONS`.
- **Estado:** ✅ Corregido.

---

### Dimensión 2 — Calidad del código generado por LLM

**🔴 CRÍTICO — Contradicción documentación vs código (download token hashing) — ✅ CORREGIDO**
- **Dónde:** `docgen/handler.go:87`
- **Problema original:** `HashToken()` existía pero no se llamaba en ningún lado.
- **Fix:** `Upload()` ahora llama `security.HashToken(token)` y guarda el hash. Migración k006 agregó columna `token_hash`.
- **Estado:** ✅ Corregido.

**🟠 ALTO — Rate limit puede dar falso positivo por error de Commit — ✅ CORREGIDO**
- **(Ya documentado en D1, corregido)**

**🟠 ALTO — No hay `context.Context` en ninguna query SQL — ✅ CORREGIDO**
- **Dónde:** `handler.go`, `cleanup.go`, `ratelimit.go`, `main.go`
- **Problema original:** Ninguna llamada SQL usaba context.
- **Fix:** Todas las queries migradas a `QueryRowContext`, `ExecContext`, `BeginTx`, `QueryContext`. Handlers HTTP usan `r.Context()`, goroutines usan `context.Background()`.

**🟡 MEDIO — Race condition en template reload mode — ✅ CORREGIDO**
- **Dónde:** `template/template.go:27`
- **Problema original:** Acceso concurrente a `e.templates` sin sincronización.
- **Fix:** `Engine` tiene `sync.RWMutex`. `Render()`, `RenderFragment()`, `RenderString()` usan `mu.Lock()` para recarga y `mu.RLock()` para lectura.

**⚪ INFO — Duplicación de lógica: cleanup de rate limits — ✅ CORREGIDO**
- **Dónde:** `cleanup/cleanup.go:24`
- **Problema original:** Dos implementaciones del mismo cleanup.
- **Fix:** `cleanup.Run()` ahora llama a `ratelimit.Cleanup(db)` en vez de tener su propia implementación.

**⚪ INFO — `RenderString` retorna string vacío sin error si el template no existe**
- **Dónde:** `template/template.go:135-137`
- **Problema:** Si un template no existe, `RenderString` retorna `"", nil`. Quien llama no puede distinguir entre "template existe y está vacío" vs "template no existe". Esto puede ocultar errores de configuración.
- **Recomendación:** Retornar error si el template no existe, o al menos loggear un warning.

---

### Dimensión 3 — Flujo de pago

**🟠 ALTO — Handler de pago solo funciona en dev mode — PENDIENTE**
- **Dónde:** `docgen/handler.go:291-318` (`Pay`)
- **Problema:** En dev mode, `Pay()` saltea Flow.cl y genera ZIP directamente. El flujo real con Flow.cl (producción) existe como stub (`createFlowPayment`) pero no se ha probado con credenciales reales. El webhook handler existe pero usa una lógica de payment que podría fallar con el formato real de Flow.cl.
- **Recomendación:** Probar con sandbox de Flow.cl, ajustar parseo del webhook según documentación real de Flow.cl.

**🟠 ALTO — No hay transacción atómica que vincule payment.confirmed ↔ download.paid — ✅ CORREGIDO**
- **Dónde:** `docgen/handler.go:442-445` (`generateAndServe`)
- **Problema original:** Dos `db.Exec()` separados sin transacción.
- **Fix:** `generateAndServe()` ahora usa transacción explícita (`h.DB.Begin()` + `defer tx.Rollback()` + `tx.Commit()`).
  ```go
  db.Exec("UPDATE payments SET status = 'confirmed' WHERE id = ?", paymentID)
  db.Exec("UPDATE downloads SET status = 'paid' WHERE id = ?", downloadID)
  ```
  Sin transacción, si el servidor cae después de la primera UPDATE pero antes de la segunda, queda un payment confirmado con download pendiente. El usuario pagó pero no puede descargar.
- **Recomendación:** Envolver ambas actualizaciones en una transacción:

  ```go
  tx, _ := db.Begin()
  defer tx.Rollback()
  tx.Exec("UPDATE payments SET status = 'confirmed', confirmed_at = datetime('now') WHERE id = ? AND status = 'pending'", paymentID)
  tx.Exec("UPDATE downloads SET status = 'paid', paid_at = datetime('now') WHERE id = ?", downloadID)
  tx.Commit()
  ```

**🟡 MEDIO — No hay mecanismo de reconsulta si webhook nunca llega**
- **Dónde:** No implementado
- **Problema:** Si Flow.cl no envía el webhook (timeout de red, error interno), el pago queda en estado "pending" para siempre. Cleanup lo borra a las 24h. El usuario pagó en Flow.cl pero no recibe su descarga. No hay endpoint para que el usuario reconulte su pago (ej: GET /status?token=...).
- **Recomendación:** Agregar endpoint de polling:
  ```
  GET /status/{download_token} → consulta payments.status y downloads.status
  ```
  Y en el frontend, redirigir a esta URL después del pago para que el usuario vea el estado.

**🟡 MEDIO — El download token debería marcarse como usado DESPUÉS de servir el archivo, no antes**
- **Dónde:** Handler de descarga (no implementado)
- **Problema:** Si se marca como usado antes de escribir la respuesta y el servidor crashea durante `io.Copy`, el usuario pagó pero no descargó y no puede reintentar. Si se marca después y el servidor crashea antes de marcar, el usuario puede descargar múltiples veces (abuso).
- **Recomendación:** Usar un patrón de "soft delete":
  1. Marcar token como `downloading` (con timestamp)
  2. Servir archivo
  3. Marcar como `used` después de `io.Copy` exitoso
  4. Si `downloading` tiene más de 5 minutos sin completarse, permitir reintento

**🔵 BAJO — `idempotency_keys` no tiene TTL — ✅ CORREGIDO**
- **Dónde:** `db/migrations/k007_idempotency_ttl.sql`, `cleanup/cleanup.go:88-90`
- **Problema original:** La tabla crecía sin límite.
- **Fix:** Migración k007 agregó columna `expires_at`. `cleanup.deleteExpiredIdempotencyKeys()` borra keys expiradas cada hora.

---

### Bugs encontrados y corregidos durante desarrollo

**🐛 CSP bloqueaba HTMX — CORREGIDO**
- **Dónde:** `middleware/security.go:10`
- **Problema:** `script-src` incluía `'self'`, `cdn.tailwindcss.com` y `'unsafe-inline'` pero NO incluía `unpkg.com` (de donde se carga HTMX). El navegador bloqueaba la descarga del script HTMX, todos los `hx-*` atributos eran ignorados, los botones de marcadores no funcionaban.
- **Fix:** Agregar `https://unpkg.com` a `script-src` en la CSP.
- **Gravedad:** 🔴 CRÍTICO (app parecía funcionar pero toda interacción HTMX estaba rota)

**🐛 hx-vals incompatible con handler Go — CORREGIDO**
- **Dónde:** `views/tools/docgen-show.html:12`, `docgen/handler.go:177-212`
- **Problema:** `hx-vals='{"marker": "{{.Name}}"}'` envía los datos como `application/json` en el body, pero `ToggleFileNameMarker` usa `r.FormValue("marker")` que solo parsea `application/x-www-form-urlencoded`. El handler nunca recibía el marcador, el toggle no funcionaba.
- **Fix:** Cambiado a `<form hx-post=...>` con `<input type="hidden" name="marker" value="{{.Name}}">`. HTMX serializa el form como form-urlencoded automáticamente.
- **Gravedad:** 🔴 CRÍTICO (funcionalidad de marcadores completamente rota)

**🐛 Tailwind CDN con `unsafe-inline` — CORREGIDO**
- **Dónde:** `middleware/security.go:10`, `views/layout.html:7`
- **Problema:** Tailwind se cargaba desde CDN via `<script>`, lo que requería `'unsafe-inline'` en CSP y exponía a riesgo de XSS si el CDN era comprometido.
- **Fix:** Tailwind compilado a `static/tailwind.css` (v4, ~4KB), servido desde `'self'`. CSP limpiada: sin `unsafe-inline`, sin CDN de Tailwind.
- **Gravedad:** 🟡 MEDIO (riesgo de seguridad, no funcional)

---

## Top acciones pendientes

1. **🟠 Handler de pago real con Flow.cl.** Probar sandbox, ajustar parseo del webhook según documentación real.

2. **🟡 Tests automatizados.** Sin tests, cualquier refactor es riesgoso.

3. **🟡 CI/CD.** Automatizar build + deploy on push a main.

---

## Preguntas abiertas (requieren más código o configuración)

1. **¿Flow.cl en producción usa el mismo secreto HMAC para webhook y creación de pago?** El `FlowSecretKey` se usa para ambos. Si es el mismo, el `VerifyHMAC` necesita confirmar que el body del webhook incluye el `flow_token` y que coincide con el esperado. Sin ver el formato del webhook de Flow.cl, no se puede auditar completamente.

2. **¿Hay un frontend JS que genera la idempotency_key?** La idempotencia actual depende del cliente. Si el frontend no genera un UUID y lo envía como campo oculto, la protección no funciona. ¿Quién genera la key — el servidor (devolviéndola en un campo oculto del form) o el cliente?

3. **¿Se usará un reverse proxy (Nginx/Caddy) en producción?** `setCookie()` ahora recibe `secure` directamente de `cfg.IsProduction()`, y `HTTPSRedirect` confía en `X-Forwarded-Proto`. La topología de red planeada (proxy → app server) debería funcionar sin cambios.

4. **¿Hay plan de agregar un challenge anti-bot (Turnstile/reCAPTCHA) antes del pago?** Sin login, el rate limiting por IP es la única defensa contra scraping/abuso. Para herramientas de $3.000 CLP, un bot que consume el trial rate limit puede bloquear a usuarios legítimos detrás de la misma NAT.
