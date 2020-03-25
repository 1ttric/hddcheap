FROM node:lts-slim AS build
WORKDIR /app/src
COPY . .
RUN yarn build --verbose

FROM nginx:1.16.1-alpine
COPY --from=build /app/src/build /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]