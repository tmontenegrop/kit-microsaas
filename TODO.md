# TODO kit-microsaas

## Urgente (antes de producción)

- [ ] Guardar download tokens hasheados — usar `token_hash` columna existente
- [ ] Arreglar `ratelimit.Check()` — propagar errores de Commit
- [ ] Hacer atómica la actualización payment+download en webhook
- [ ] Arreglar `isRequestSecure()` para reverse proxy
- [ ] Cleanup race condition — `UPDATE status='expired'` en vez de DELETE

## Corto plazo

- [ ] Compilar Tailwind a CSS estático, limpiar CSP
- [ ] Implementar creación de pago real en Flow.cl
- [ ] Implementar webhook real con idempotencia
- [ ] Health check endpoint
- [ ] Dockerizar la app

## Mediano plazo

- [ ] Context en todas las queries SQL
- [ ] Mutex en template reload mode
- [ ] Backup automático de SQLite
- [ ] Endpoint de polling `/status/{token}`
- [ ] Soft-delete de tokens de descarga

## Largo plazo

- [ ] CI/CD
- [ ] Segunda herramienta
- [ ] Métricas y dashboard admin
