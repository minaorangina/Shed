module.exports = {
  entry: './src/index.ts',
  output: {
    filename: 'dist/bundle.js'
  },
  devtool: 'source-map', // or inline-source-map?  
  resolve: {
      extensions: ['.ts', '.js', '.json']
  },
  module: {
    rules: [
      { test: /\.ts$/, use: 'awesome-typescript-loader' }
    ]
  }
}
