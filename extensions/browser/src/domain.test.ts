import { describe, expect, test } from 'bun:test';
import { domainOf } from './domain';

describe('domainOf', () => {
  test('returns only the lowercased hostname', () => {
    expect(domainOf('https://News.Example.com:8443/a/b?token=secret#frag')).toBe(
      'news.example.com',
    );
    expect(domainOf('http://localhost:3000/')).toBe('localhost');
  });

  test('ignores pages that are not web pages', () => {
    expect(domainOf('about:blank')).toBeUndefined();
    expect(domainOf('chrome://settings')).toBeUndefined();
    expect(domainOf('moz-extension://abc/page.html')).toBeUndefined();
    expect(domainOf('file:///home/me/notes.txt')).toBeUndefined();
  });

  test('ignores missing or malformed URLs', () => {
    expect(domainOf(undefined)).toBeUndefined();
    expect(domainOf('')).toBeUndefined();
    expect(domainOf('not a url')).toBeUndefined();
  });
});
