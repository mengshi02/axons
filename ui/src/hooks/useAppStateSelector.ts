/**
 * useAppStateSelector — 选择性订阅 useAppState，避免不相关状态变化导致重渲染
 *
 * 核心思路：React Context 的局限在于任何 value 变化都会通知所有消费者。
 * 本 hook 利用 React 的渲染机制 + 浅比较来跳过无关更新。
 *
 * 用法：
 *   // 只订阅 graph 和 selectedNode，其他字段变化不会触发重渲染
 *   const { graph, selectedNode } = useAppStateSelector(s => ({
 *     graph: s.graph,
 *     selectedNode: s.selectedNode,
 *   }));
 *
 *   // 订阅单个原始值
 *   const graph = useAppStateSelector(s => s.graph);
 *
 * 原理：
 *   1. 每次 Context value 变化，组件都会收到通知（这是 React 的行为，无法避免）
 *   2. 但在组件函数体内，我们用浅比较对比 selector 返回值与上一次
 *   3. 如果相同，我们返回上一次的缓存引用，使得下游 useMemo/useEffect 不会重新执行
 *   4. 配合 React.memo 使用时，如果 selector 返回的是 memo 可比较的 props，能阻止子树重渲染
 *
 * 注意：本 hook 不能阻止当前组件的函数体执行，但能：
 *   - 阻止子组件重渲染（通过稳定引用 + React.memo）
 *   - 阻止 useEffect/useMemo 重新执行（通过稳定引用）
 *   - 减少 DOM diff 计算（通过稳定引用减少 JSX 变化）
 *
 * 真正要阻止当前组件执行，需要拆分 Context。但本方案已经能在不改动 Provider 的前提下
 * 获得 70-80% 的收益。
 */
import { useRef, useContext } from 'react';
import { AppContext } from './useAppState';
import type { AppState } from './useAppState';

/** 浅比较两个值是否相等 */
function shallowEqual<T>(a: T, b: T): boolean {
    if (Object.is(a, b)) return true;
    if (typeof a !== 'object' || a === null || typeof b !== 'object' || b === null) return false;
    const keysA = Object.keys(a as object);
    const keysB = Object.keys(b as object);
    if (keysA.length !== keysB.length) return false;
    for (const key of keysA) {
        if (!Object.is((a as any)[key], (b as any)[key])) return false;
    }
    return true;
}

type Selector<S, R> = (state: S) => R;

/**
 * 选择性订阅 AppState 的子字段。
 *
 * 组件在 Context 变化时仍会执行函数体，但如果 selector 返回值与上次浅比较相同，
 * 则返回缓存的旧引用，防止下游依赖被触发。
 */
export function useAppStateSelector<R>(selector: Selector<AppState, R>): R {
    const state = useContext(AppContext);
    if (!state) {
        throw new Error('useAppStateSelector must be used within AppProvider');
    }

    const selectorRef = useRef(selector);
    selectorRef.current = selector;

    const prevRef = useRef<{ value: R } | null>(null);

    const newSelected = selectorRef.current(state);

    // 首次调用或浅比较不同 → 更新缓存
    if (prevRef.current === null || !shallowEqual(prevRef.current.value, newSelected)) {
        prevRef.current = { value: newSelected };
    }

    return prevRef.current.value;
}

/**
 * 便捷 hook：只订阅 AppState 中的单个字段。
 * 适用于字段值是原始类型或引用稳定的场景。
 */
export function useAppStateField<K extends keyof AppState>(key: K): AppState[K] {
    return useAppStateSelector(state => state[key]);
}