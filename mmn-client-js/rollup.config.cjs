const resolve = require('@rollup/plugin-node-resolve');
const commonjs = require('@rollup/plugin-commonjs');
const json = require('@rollup/plugin-json');
const typescript = require('@rollup/plugin-typescript');
const { dts } = require('rollup-plugin-dts');

const packageJson = require('./package.json');

// External dependencies that should not be bundled
const external = ['crypto', 'axios', 'bs58', 'bip39', 'tweetnacl'];

module.exports = [
  // ES Module build
  {
    input: 'src/index.ts',
    output: {
      file: packageJson.module,
      format: 'esm',
      sourcemap: true,
    },
    plugins: [
      resolve({
        preferBuiltins: true,
        browser: false,
      }),
      commonjs(),
      json(),
      typescript({
        tsconfig: './tsconfig.json',
        exclude: ['**/*.test.ts', '**/*.spec.ts'],
        declaration: false, // Let dts plugin handle declarations
        declarationMap: false,
      }),
    ],
    external,
  },
  // CommonJS build
  {
    input: 'src/index.ts',
    output: {
      file: packageJson.main,
      format: 'cjs',
      sourcemap: true,
      exports: 'auto',
    },
    plugins: [
      resolve({
        preferBuiltins: true,
        browser: false,
      }),
      commonjs(),
      json(),
      typescript({
        tsconfig: './tsconfig.json',
        exclude: ['**/*.test.ts', '**/*.spec.ts'],
        declaration: false, // Let dts plugin handle declarations
        declarationMap: false,
      }),
    ],
    external,
  },
  // TypeScript declarations
  {
    input: 'src/index.ts',
    output: {
      file: packageJson.types,
      format: 'esm',
    },
    plugins: [dts()],
    external: [/\.css$/, ...external],
  },
];
