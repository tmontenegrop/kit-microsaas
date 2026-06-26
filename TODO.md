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

## Pendiente (antes de producción)

- [ ] Handler de pago real con Flow.cl (probar sandbox)
- [ ] Tests automatizados
- [ ] CI/CD

## Deseable

- [ ] Backup automático de SQLite
- [ ] Soft-delete de tokens de descarga
- [ ] Segunda herramienta
- [ ] Métricas y dashboard admin
