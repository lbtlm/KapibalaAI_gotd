import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
afterEach(cleanup)
Object.defineProperty(window, 'matchMedia', { value: () => ({ matches: false, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() {} }) })
class ResizeObserver { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = ResizeObserver
const computedStyle = window.getComputedStyle.bind(window); window.getComputedStyle = (element) => computedStyle(element)
