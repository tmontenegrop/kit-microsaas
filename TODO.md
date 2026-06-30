# TODO kit-microsaas

## Seguridad (todo listo ✅)

| Hallazgo | Estado |
|----------|--------|
| Download tokens hasheados | ✅ |
| ratelimit.Check() propaga errores | ✅ |
| Payment+download atómico | ✅ |
| isRequestSecure() para proxy | ✅ |
| Cleanup race condition | ✅ |
| Tailwind estático + CSP sin unsafe-inline | ✅ |
| CORS methods limitados | ✅ |
| CSRF error logging | ✅ |
| TTL en idempotency_keys | ✅ |
| context.Context en queries SQL | ✅ |
| Template reload race condition (mutex) | ✅ |

## Segunda ronda auditoría (30 jun 2026)

| Hallazgo | Gravedad | Estado |
|----------|----------|--------|
| C1: generateAndServe retorna error | 🔴 Crítico | ✅ |
| C3: Webhook reordenado (ZIP antes que confirmación) | 🔴 Crítico | ✅ |
| H1: DataUpload verifica UPDATE error | 🟠 Alto | ✅ |
| H2: Show verifica batchPrice query error | 🟠 Alto | ✅ |
| H3: recordTrialUsage antes de generateAndServe | 🟠 Alto | ✅ |
| H4: Idempotencia en Pay | 🟠 Alto | ✅ |
| M2: Status endpoint retorna 404 correcto | 🟡 Medio | ✅ |
| M3: Rate limit en DataUpload y Pay | 🟡 Medio | ✅ |
| B1: Error Upload no expone paths | 🔵 Bajo | ✅ |
| B2: CSP sin data: en img-src | 🔵 Bajo | ✅ |
| Plan inválido retorna 400 | 🔵 Bajo | ✅ |
| Excel: GetSheetList()[0] en vez de "Sheet1" | 🔵 Bajo | ✅ |
| flow_token en generateAndServe | 🔵 Bajo | ✅ |

## Implementado (30 jun 2026)

| Feature | Estado |
|---------|--------|
| ratelimit.Check() conectado en Upload | ✅ |
| Cookie device_id en primer upload | ✅ |
| Free trial: 30 docs/30 días/IP | ✅ |
| Plan Batch ($2.990) | ✅ |
| Plan Pase 30 días ($6.990) | ✅ |
| getTrialInfo() + recordTrialUsage() | ✅ |
| recordPass() whitelist IP por 30 días | ✅ |
| Precio desde DB (tools.price_clp) | ✅ |
| Límite 300 filas por Excel | ✅ |
| UI con 3 estados (pass/trial/agotado) | ✅ |
| Migración k010_trial_pass | ✅ |
| Webhook maneja pass (amount >= 6990) | ✅ |
| k009 leftover eliminado | ✅ |
| Idempotencia en Pay (key pay:{idk}) | ✅ |
| Rate limit DataUpload (20 req/h) | ✅ |
| Rate limit Pay (10 req/h) | ✅ |
| generateAndServe acepta flowToken | ✅ |

## Pendiente (antes de producción)

- [ ] Handler de pago real con Flow.cl (probar sandbox)
- [ ] Tests automatizados
- [ ] CI/CD

## Deseable

- [ ] Backup automático de SQLite
- [ ] Soft-delete de tokens de descarga
- [ ] Segunda herramienta
- [ ] Métricas y dashboard admin
