# Roadmap a Producción — kit-microsaas

**Meta:** Tener DocGen funcionando en producción (Chile, Flow.cl, HTTPS) con riesgos financieros controlados.

## Fase 1 — Seguridad crítica (antes de abrir al público)

| # | Tarea | Prioridad | Estado |
|---|-------|-----------|--------|
| 1 | Guardar download tokens hasheados (`token_hash`) en vez de texto plano | 🔴 CRÍTICO | ✅ |
| 2 | Arreglar `ratelimit.Check()` — propagar errores de Commit | 🔴 CRÍTICO | ✅ |
| 3 | Hacer atómica la actualización payment+download en webhook | 🟠 ALTO | ✅ |
| 4 | Arreglar `isRequestSecure()` para reverse proxy | 🟠 ALTO | ✅ |
| 5 | Cleanup race condition: usar `UPDATE status='expired'` en vez de DELETE | 🟠 ALTO | ✅ |
| 6 | Compilar Tailwind a CSS estático, remover `unsafe-inline` de CSP | 🟠 ALTO | ✅ |
| 7 | Hacer cleanup de payments + downloads atómico (transacción) | 🟠 ALTO | ✅ |
| 8 | Loggear error si `crypto/rand` falla en CSRF token | 🟡 MEDIO | ✅ |
| 9 | Agregar TTL a `idempotency_keys` | 🟡 MEDIO | ✅ |
| 10 | Limitar CORS a GET, POST, OPTIONS | 🔵 BAJO | ✅ |

## Fase 2 — Integración Flow.cl real

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 11 | Implementar handler de creación de pago en Flow.cl (`/api/payment/create`) | 🔴 CRÍTICO | 3 |
| 12 | Implementar webhook handler real con idempotencia | 🔴 CRÍTICO | 3 |
| 13 | Agregar endpoint de polling `/status/{token}` para reconsulta post-pago | 🟡 MEDIO | 11 |
| 14 | Soft-delete de tokens de descarga (estado `downloading` con timeout) | 🟡 MEDIO | 1 |

## Fase 3 — Monitoreo y operaciones

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 15 | Agregar health check endpoint (`GET /health`) | 🟡 MEDIO | — |
| 16 | Logs estructurados a archivo rotado (o stdout + systemd) | 🟡 MEDIO | — |
| 17 | Métricas básicas: requests, errores 5xx, payments, rate limits alcanzados | 🟡 MEDIO | — |
| 18 | Alerta si webhook no llega después de N minutos del pago | 🟡 MEDIO | 12 |
| 19 | Dashboard de estado de pagos (solo admin, con token fijo) | 🔵 BAJO | 12 |

## Fase 4 — Deploy

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 20 | Dockerizar la app (multi-stage build Go + SQLite) | 🟠 ALTO | 1-10 |
| 21 | Configurar reverse proxy (Caddy o Nginx) con TLS automático | 🟠 ALTO | 20 |
| 22 | Setup de dominio + DNS (ej: docgen.kit.app) | 🟠 ALTO | 21 |
| 23 | Configurar variables de entorno producción (`ENV=production`, `FLOW_API_KEY`, etc.) | 🟠 ALTO | 22 |
| 24 | Backup automático de SQLite (cron diario + antes de migraciones) | 🟡 MEDIO | 20 |
| 25 | CI/CD básico (push a main → build → deploy) | 🔵 BAJO | 20 |

## Fase 5 — Escalabilidad y optimización

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 26 | Migrar contexto a todas las queries SQL (`BeginTx`, `ExecContext`) | 🟡 MEDIO | — |
| 27 | Mutex en template reload mode (race condition en desarrollo) | 🟡 MEDIO | — |
| 28 | Cache de templates en producción (sin reload) | 🟡 MEDIO | — |
| 29 | Evaluar SQLite WAL mode para mejor concurrencia | 🔵 BAJO | — |
| 30 | Límite de tamaño en data_rows JSON (evitar DB gigante) | 🔵 BAJO | — |
| 31 | Compilar HTMX + Tailwind a archivos estáticos (sin CDN) | 🔵 BAJO | 6 |

## Fase 6 — Post-lanzamiento

| # | Tarea | Prioridad | Depende de |
|---|-------|-----------|------------|
| 32 | Monitorear logs de errores las primeras 48h | 🟠 ALTO | 15-19 |
| 33 | Encuesta anónima opcional post-descarga (1 pregunta) | 🔵 BAJO | — |
| 34 | Agregar segunda herramienta (ej: Calculadora de finiquito) | 🔵 BAJO | — |
| 35 | Evaluar cambio a hosting estático + Worker/Edge para reducir costos | 🔵 BAJO | 34 |
