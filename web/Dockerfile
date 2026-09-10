# node:22-alpine 构建 → nginx:alpine 托管
FROM node:22-alpine AS build
WORKDIR /app
# 依赖层单独缓存（同后端思路）
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
