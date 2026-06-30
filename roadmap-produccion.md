# Roadmap a Producción — kit-microsaas

**Meta:** Tener DocGen funcionando en producción (Chile, Flow.cl, HTTPS) con riesgos financieros controlados.

## Fase 1 — Seguridad crítica (completada ✅)

| # | Tarea | Estado |
|---|-------|--------|
| 1 | Guardar download tokens hasheados (`token_hash`) en vez de texto plano | ✅ |
| 2 | Arreglar `ratelimit.Check()` — propagar errores de Commit | ✅ |
| 3 | Hacer atómica la actualización payment+download en webhook | ✅ |
| 4 | Arreglar `isRequestSecure()` para reverse proxy | ✅ |
| 5 | Cleanup race condition: usar `UPDATE status='expired'` en vez de DELETE | ✅ |
| 6 | Compilar Tailwind a CSS estático, remover `unsafe-inline` de CSP | ✅ |
| 7 | Hacer cleanup de payments + downloads atómico (transacción) | ✅ |
| 8 | Loggear error si `crypto/rand` falla en CSRF token | ✅ |
| 9 | Agregar TTL a `idempotency_keys` | ✅ |
| 10 | Limitar CORS a GET, POST, OPTIONS | ✅ |

## Fase 1b — Free trial + Planes (completada ✅)

| # | Tarea | Estado |
|---|-------|--------|
| 11 | Conectar `ratelimit.Check()` en Upload handler | ✅ |
| 12 | Cookie `device_id` en primer upload | ✅ |
| 13 | Free trial: 30 docs/30 días rolling por IP (`trial_tracking` table) | ✅ |
| 14 | Plan Batch ($2.990) con límite 300 filas | ✅ |
| 15 | Plan Pase 30 días ($6.990, unlimited con rate limit diario) | ✅ |
| 16 | UI con 3 estados: pass activo, trial disponible, trial agotado | ✅ |
| 17 | Precio centralizado en DB (`tools.price_clp`) | ✅ |
| 18 | Webhook maneja pass payment (amount >= 6990 → recordPass) | ✅ |
| 19 | Migración k010_trial_pass.sql | ✅ |

## Fase 1c — Segunda ronda auditoría (completada ✅)

| # | Tarea | Gravedad | Estado |
|---|-------|----------|--------|
| 20 | C1: generateAndServe debe retornar error en vez de tragarse fallos | 🔴 CRÍTICO | ✅ |
| 21 | C3: Webhook debe generar ZIP antes de confirmar pago (orden inverso) | 🔴 CRÍTICO | ✅ |
| 22 | H1: DataUpload verificar error de UPDATE downloads | 🟠 ALTO | ✅ |
| 23 | H2: Show verificar error de QueryRow para batchPrice | 🟠 ALTO | ✅ |
| 24 | H3: Pay free: recordTrialUsage antes de generateAndServe | 🟠 ALTO | ✅ |
| 25 | H4: Idempotencia en Pay (key pay:{idk}) | 🟠 ALTO | ✅ |
| 26 | M2: Status endpoint: 404 en vez de template incorrecto | 🟡 MEDIO | ✅ |
| 27 | M3: Rate limit en DataUpload (20 req/h) y Pay (10 req/h) | 🟡 MEDIO | ✅ |
| 28 | B1: No exponer paths del sistema en Upload error | 🔵 BAJO | ✅ |
| 29 | B2: CSP remover `data:` de img-src | 🔵 BAJO | ✅ |
| 30 | Plan inválido retorna 400 en vez de tratarse como batch | 🔵 BAJO | ✅ |
| 31 | Excel: GetSheetList()[0] en vez de hardcode "Sheet1" | 🔵 BAJO | ✅ |
| 32 | generateAndServe acepta flowToken y lo guarda en payments | 🔵 BAJO | ✅ |

## Fase 2 — Integración Flow.cl real

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 33 | Probar sandbox Flow.cl (`/api/payment/create` real) | 🔴 CRÍTICO | 1-32 |
| 34 | Verificar webhook handler con respuestas reales de Flow.cl | 🔴 CRÍTICO | 33 |
| 35 | Probar flujo completo: batch $2.990 → Flow.cl → webhook → descarga | 🟠 ALTO | 34 |
| 36 | Probar flujo completo: pass $6.990 → Flow.cl → webhook → whitelist + descarga | 🟠 ALTO | 34 |
| 37 | Soft-delete de tokens de descarga (estado `downloading` con timeout) | 🟡 MEDIO | 1 |

## Fase 3 — Monitoreo y operaciones

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 38 | Logs estructurados a archivo rotado (o stdout + systemd) | 🟡 MEDIO | — |
| 39 | Métricas básicas: requests, errores 5xx, payments, rate limits alcanzados | 🟡 MEDIO | — |
| 40 | Alerta si webhook no llega después de N minutos del pago | 🟡 MEDIO | 33-36 |
| 41 | Dashboard de estado de pagos (solo admin, con token fijo) | 🔵 BAJO | 33-36 |

## Fase 4 — Deploy

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 42 | Dockerizar la app (multi-stage build Go + SQLite) | 🟠 ALTO | ✅ (hecho) |
| 43 | Configurar reverse proxy (Caddy) con TLS automático | 🟠 ALTO | ✅ (hecho) |
| 44 | Setup de dominio + DNS (ej: docgen.kit.app) | 🟠 ALTO | 43 |
| 45 | Configurar variables de entorno producción (`ENV=production`, `FLOW_API_KEY`, etc.) | 🟠 ALTO | 44 |
| 46 | Backup automático de SQLite (cron diario + antes de migraciones) | 🟡 MEDIO | ✅ (script hecho) |
| 47 | CI/CD básico (push a main → build → deploy) | 🔵 BAJO | 42 |

## Fase 5 — Post-lanzamiento

| # | Tarea | Prioridad |
|---|-------|-----------|
| 48 | Monitorear logs de errores las primeras 48h | 🟠 ALTO |
| 49 | Encuesta anónima opcional post-descarga (1 pregunta) | 🔵 BAJO |
| 50 | Agregar segunda herramienta (ej: Calculadora de finiquito) | 🔵 BAJO |
| 51 | Evaluar rate limit diario para pase (~5 subidas/día) | 🟡 MEDIO |
