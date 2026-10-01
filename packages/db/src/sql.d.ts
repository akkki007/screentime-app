// `.sql` files are imported as text (`with { type: 'text' }`) by migrations.ts.
declare module '*.sql' {
  const text: string;
  export default text;
}
