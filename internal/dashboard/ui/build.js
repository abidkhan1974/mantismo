const esbuild = require('esbuild');
const path = require('path');
const fs = require('fs');

// Ensure dist exists
fs.mkdirSync('dist', { recursive: true });

// Copy index.html to dist
const html = fs.readFileSync('index.html', 'utf8');
fs.writeFileSync('dist/index.html', html);

// Build JS bundle
esbuild.build({
  entryPoints: ['src/main.jsx'],
  bundle: true,
  outfile: 'dist/bundle.js',
  minify: true,
  target: ['chrome90', 'firefox88', 'safari14'],
  define: {
    'process.env.NODE_ENV': '"production"'
  },
}).then(() => {
  console.log('Build complete');
}).catch(() => process.exit(1));
