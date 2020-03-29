const {createProxyMiddleware} = require('http-proxy-middleware');
module.exports = function (app) {
    app.use(
        '/ws',
        createProxyMiddleware({
            target: 'http://127.0.0.1:3001',
            ws: true
        })
    );
};