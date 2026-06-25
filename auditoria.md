# Auditoría de kit-microsaas

**Auditor:** LLM Auditor | **Fecha:** 2026-06-25 (v2) | **Archivos auditados:** 11 `.go` + 5 `.sql` + `AGENTS.md`

---

## Resumen ejecutivo

El kit es sorprendentemente bueno para un proyecto generado por LLM: las decisiones arquitectónicas son coherentes, la superficie de ataque se redujo drásticamente al eliminar auth/email/roles, y los controles de seguridad básicos (CSRF, path traversal, parámetros preparados) están correctos. Sin embargo, hay contradicciones graves entre la documentación y el código (el schema guarda tokens en texto plano, la doc dice "hashed"), bugs de concurrencia en rate limiting que pueden dejar dinero en la mesa, y el cleanup race condition puede borrar descargas pagadas. El riesgo más crítico es que **un pago confirmado y una descarga pueden perder su correlación** por falta de transacciones atómicas. Recomendación: no poner en producción sin resolver los 🔴.

---

## Hallazgos por dimensión

### Dimensión 1 — Seguridad

**🔴 CRÍTICO — Token de descarga almacenado en texto plano (contradice la documentación)**
- **Dónde:** `db/migrations/k003_downloads.sql:4`, `security/token.go:41`
- **Problema:** AGENTS.md dice "Stored hashed (SHA-256)" y `HashToken()` existe, pero la migración define `token TEXT UNIQUE NOT NULL` sin hash. `HashToken()` nunca se llama en ningún lado. El token se guarda en texto plano. Si la DB se filtra, cualquier atacante puede descargar archivos pagados por otros.
- **Evidencia:** En `k003_downloads.sql:4` → `token TEXT UNIQUE NOT NULL`. En `cleanup.go:48` se usa `token` directamente para construir paths de archivos. En ningún lugar se invoca `security.HashToken()`.
- **Recomendación:**
  ```sql
  -- Añadir columna token_hash
  ALTER TABLE downloads ADD COLUMN token_hash TEXT UNIQUE;
  -- O cambiando la migracion:
  token_hash TEXT UNIQUE NOT NULL,
  -- El token original solo viaja en URL de redirect y nunca se persiste
  ```

**🔴 CRÍTICO — `ratelimit.Check()` traga error de Commit (falso positivo)**
- **Dónde:** `ratelimit/ratelimit.go:28,40`
- **Problema:** `return tx.Commit() == nil, nil` convierte un error de Commit en "rate limit passed". Si la DB falla al escribir, el atacante puede disparar requests ilimitados.
- **Evidencia:**
  ```go
  // linea 28
  return tx.Commit() == nil, nil  // si Commit falla, retorna (true, nil)
  // linea 40 (mismo patron)
  return tx.Commit() == nil, nil  // mismo bug
  // linea 44 (correcto)
  return false, tx.Commit()  // este sí propaga el error
  ```
- **Recomendación:**
  ```go
  // Reemplazar todos los return tx.Commit() == nil, nil por:
  if err := tx.Commit(); err != nil {
      return false, err
  }
  return true, nil
  ```

**🟠 ALTO — `isRequestSecure()` no funciona detrás de reverse proxy en producción**
- **Dónde:** `csrf/csrf.go:45-56`
- **Problema:** La función solo confía en `X-Forwarded-Proto` si el RemoteAddr es 127.0.0.1 o ::1. En producción real (Nginx/Caddy en el mismo servidor o diferente), RemoteAddr será la IP del proxy o la IP pública. La cookie CSRF no tendrá Secure flag.
- **Evidencia:** `csrf.go:51-52`: `strings.HasPrefix(host, "127.0.0.1:") || strings.HasPrefix(host, "[::1]:")`
- **Recomendación:** Eliminar la restricción de localhost. Si el proxy setea `X-Forwarded-Proto`, confiar en él sin importar RemoteAddr. O mejor: usar `cfg.IsProduction()` directamente:
  ```go
  func setCookie(w http.ResponseWriter, r *http.Request, token string, secure bool) {
      // secure ahora viene de cfg.IsProduction(), no de isRequestSecure()
      ...
      Secure: secure,
  ```

**🟠 ALTO — Cleanup race condition: puede borrar un download recién pagado**
- **Dónde:** `cleanup/cleanup.go:30-52`
- **Problema:** La query selecciona downloads sin payment confirmado con `created_at < 24h`. Si el webhook de Flow.cl llega durante la ejecución del cleanup (ventana de milisegundos), el download se borra aunque el pago se confirmó. Usuario pierde dinero.
- **Evidencia:** La query en línea 34-35: `LEFT JOIN payments p ON p.download_id = d.id AND p.status = 'confirmed' WHERE p.id IS NULL AND d.created_at < ?`. El DELETE posterior (línea 66) no verifica el estado actual.
- **Recomendación:** En vez de DELETE, marcar como `status = 'expired'`. O usar `UPDATE ... SET status = 'expired' WHERE status = 'pending' AND ...` y verificar `RowsAffected` antes de borrar archivos.

**🟠 ALTO — Cleanup borra payments sin transacción atómica**
- **Dónde:** `cleanup/cleanup.go:61-68`
- **Problema:** `DELETE FROM payments` y `DELETE FROM downloads` son statements separados. Si el proceso muere entre ambos, quedan payments huérfanos (download_id que ya no existe). Inconsistencia financiera.
- **Recomendación:** Envolver ambos DELETES en una transacción:
  ```go
  tx, _ := db.Begin()
  defer tx.Rollback()
  tx.Exec("DELETE FROM payments WHERE download_id = ?", id)
  tx.Exec("DELETE FROM downloads WHERE id = ?", id)
  tx.Commit()
  ```

**🟡 MEDIO — CSRF token silenciosamente falla si crypto/rand falla**
- **Dónde:** `csrf/csrf.go:85-89`
- **Problema:** Si `GenerateToken()` falla (extremadamente raro, pero posible en entornos con bajo entropy), el error se ignora. El request sigue sin CSRF token en la cookie, haciendo que el próximo POST falle con "CSRF token missing".
- **Evidencia:**
  ```go
  token, err = GenerateToken()
  if err == nil {
      setCookie(w, r, token, secure)
  }
  ```
- **Recomendación:** Al menos loggear el error. En producción severa, retornar 500:
  ```go
  if err != nil {
      slog.Error("csrf generate token", "error", err)
      http.Error(w, "Error interno", http.StatusInternalServerError)
      return
  }
  ```

**🟡 MEDIO — CSP depende de CDN de terceros con `unsafe-inline`**
- **Dónde:** `middleware/security.go:10`
- **Problema:** Tailwind CDN requiere `'unsafe-inline'` para scripts y estilos. Si el CDN es comprometido, puede inyectar código. Además, `'unsafe-inline'` anula parte de la protección CSP contra XSS.
- **Recomendación:** Antes de producción, compilar Tailwind a un archivo `.css` estático y servirlo desde `'self'`. Remover `'unsafe-inline'` de la CSP y servir el JS mínimo necesario desde `'self'`.

**🔵 BAJO — CORS expone métodos PUT/DELETE en todos los origins**
- **Dónde:** `middleware/security.go:59-60`
- **Problema:** `DefaultCORSConfig()` incluye PUT y DELETE. Para un MicroSaaS que solo hace POST y GET, estos métodos sobran y expanden superficie.
- **Recomendación:** Limitar a `GET, POST, OPTIONS`.

---

### Dimensión 2 — Calidad del código generado por LLM

**🔴 CRÍTICO — Contradicción documentación vs código (download token hashing)**
- **Dónde:** `AGENTS.md:158` vs `db/migrations/k003_downloads.sql:4`
- **Problema:** Esto es un error clásico de código generado por LLM: se escribe una función `HashToken()`, se documenta su uso, pero nunca se integra en el flujo real. El LLM generó la función porque "sonaba bien" pero el código que la llamaría nunca se generó.
- **Recomendación:** Decidir: o se guarda el token hasheado (implementar llamado en el handler de creación de download), o se acepta texto plano y se corrige la documentación.

**🟠 ALTO — Rate limit puede dar falso positivo por error de Commit**
- **(Ya documentado en D1)**

**🟠 ALTO — No hay `context.Context` en ninguna query SQL**
- **Dónde:** Todos los archivos .go
- **Problema:** Ninguna llamada a `db.Conn.QueryRow`, `db.Conn.Exec`, `db.Conn.Begin` usa `context.Context`. Si el cliente cierra la conexión, la query sigue ejecutándose. En SQLite con WAL mode no es crítico, pero es mala práctica.
- **Recomendación:** Migrar a `db.Conn.BeginTx(ctx, nil)`, `db.Conn.ExecContext(ctx, ...)`, etc.
  ```go
  func Check(ctx context.Context, db *sql.DB, key string, maxAttempts int, window time.Duration) (bool, error) {
      tx, err := db.BeginTx(ctx, nil)
  ```

**🟡 MEDIO — Race condition en template reload mode**
- **Dónde:** `template/template.go:78-81`
- **Problema:** En desarrollo (`reload=true`), `loadTemplates()` reasigna `e.templates` mientras `Render()` lo lee. Sin mutex. Dos requests paralelos pueden causar panic en lectura/escritura concurrente de map.
- **Evidencia:** `loadTemplates()` (línea 148) escribe `e.templates = make(...)`. `Render()` (línea 85) lee `e.templates[name]`. Sin sincronización.
- **Recomendación:**
  ```go
  var mu sync.RWMutex
  func (e *Engine) Render(...) {
      if e.reload {
          mu.Lock()
          e.loadTemplates()
          mu.Unlock()
      }
      mu.RLock()
      tmpl, ok := e.templates[name]
      mu.RUnlock()
  ```

**⚪ INFO — Duplicación de lógica: cleanup de rate limits**
- **Dónde:** `ratelimit/ratelimit.go:55-56` y `cleanup/cleanup.go:25-27`
- **Problema:** Existen dos funciones que hacen exactamente lo mismo. `cleanup.Run()` usa su propia implementación en vez de llamar a `ratelimit.Cleanup()`.
- **Recomendación:** Eliminar `deleteExpiredRateLimits` de cleanup.go y llamar a `ratelimit.Cleanup(db)`.

**⚪ INFO — `RenderString` retorna string vacío sin error si el template no existe**
- **Dónde:** `template/template.go:135-137`
- **Problema:** Si un template no existe, `RenderString` retorna `"", nil`. Quien llama no puede distinguir entre "template existe y está vacío" vs "template no existe". Esto puede ocultar errores de configuración.
- **Recomendación:** Retornar error si el template no existe, o al menos loggear un warning.

---

### Dimensión 3 — Flujo de pago

**🟠 ALTO — No hay handler de pago implementado, no se puede auditar el flujo real**
- **Dónde:** Todo el proyecto
- **Problema:** El flujo descrito en AGENTS.md (pasos 1-9) no existe en código. Solo hay rutas GET para landing pages. Todo el flujo de pago, webhook, descarga es teórico. Esta auditoría solo puede validar el schema y la infraestructura, no la lógica financiera.
- **Recomendación:** Implementar el flujo mínimo y re-auditar.

**🟠 ALTO — No hay transacción atómica que vincule payment.confirmed ↔ download.paid**
- **Dónde:** Schema y lógica (inexistente)
- **Problema:** Cuando llegue el webhook, el código hará algo como:
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

**🔵 BAJO — `idempotency_keys` no tiene TTL**
- **Dónde:** `db/migrations/k005_idempotency.sql`
- **Problema:** La tabla crece sin límite. Cada request de pago agrega una fila. En producción con miles de pagos, la tabla crece indefinidamente.
- **Recomendación:** Agregar columna `expires_at` o incluir idempotency_keys en el cleanup job (borrar >7 días).

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

---

## Top 3 acciones inmediatas antes de producción

1. **🔴 Guardar download tokens hasheados.** Implementar `security.HashToken()` en la creación del download y ajustar cleanup para buscar por hash. Es la contradicción más grave entre doc y código. `cleanup.go:48` necesita cambiar para no usar el token como path de archivo (usar download ID en vez de token para paths).

2. **🔴 Arreglar `ratelimit.Check()` para que propague errores de Commit.** Los dos `return tx.Commit() == nil, nil` en `ratelimit.go:28,40` rompen la seguridad anti-spam. Si la DB falla, un atacante puede disparar requests ilimitados.

3. **🟠 Hacer atómica la actualización payment+download en el webhook handler + cleanup.** Usar transacciones en ambos. Implementar el handler de webhook con idempotencia (check `IF status = 'pending'` antes de confirmar). Sin esto, hay riesgo de pérdida financiera.

---

## Preguntas abiertas (requieren más código o configuración)

1. **¿Flow.cl en producción usa el mismo secreto HMAC para webhook y creación de pago?** El `FlowSecretKey` se usa para ambos. Si es el mismo, el `VerifyHMAC` necesita confirmar que el body del webhook incluye el `flow_token` y que coincide con el esperado. Sin ver el formato del webhook de Flow.cl, no se puede auditar completamente.

2. **¿Hay un frontend JS que genera la idempotency_key?** La idempotencia actual depende del cliente. Si el frontend no genera un UUID y lo envía como campo oculto, la protección no funciona. ¿Quién genera la key — el servidor (devolviéndola en un campo oculto del form) o el cliente?

3. **¿Se usará un reverse proxy (Nginx/Caddy) en producción?** El `isRequestSecure()` bug y el `HTTPSRedirect` asumen que el app server habla directamente con el cliente o que el proxy setea `X-Forwarded-Proto`. ¿Cuál es la topología de red planeada?

4. **¿Hay plan de agregar un challenge anti-bot (Turnstile/reCAPTCHA) antes del pago?** Sin login, el rate limiting por IP es la única defensa contra scraping/abuso. Para herramientas de $3.000 CLP, un bot que consume el trial rate limit puede bloquear a usuarios legítimos detrás de la misma NAT.
