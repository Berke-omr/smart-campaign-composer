# syntax=docker/dockerfile:1

# Stages: deps -> test (used by `make test`) / build -> runtime (nginx serving the static build).

FROM node:24.22.0-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund

FROM deps AS test
COPY . .
# Type-check first (app, tests and config), so a type error fails `make test` too.
CMD ["sh", "-c", "npx vue-tsc --build && npx vitest --run"]

FROM deps AS build
COPY . .
# `npm run build` type-checks (vue-tsc) before bundling, so type errors fail the image build.
RUN npm run build

FROM nginx:1.30.6-alpine AS runtime
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
