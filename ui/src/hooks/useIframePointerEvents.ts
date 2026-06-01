import { useEffect } from 'react';

/**
 * useIframePointerEvents — 当弹窗/下拉框打开时，禁用所有插件 iframe 的指针事件，
 * 使点击穿透到宿主层，从而触发 click-outside 关闭逻辑。
 *
 * iframe 内部的点击事件不会冒泡到宿主 document，导致宿主的
 * `document.addEventListener('mousedown', handleClickOutside)` 无法感知
 * iframe 内的点击，弹窗就无法通过点击插件区域关闭。
 *
 * 临时给 iframe 加 pointer-events: none 后，点击穿透到宿主层，
 * 宿主的 click-outside 监听器正常触发。
 *
 * @param active - 是否激活（通常对应弹窗/下拉的打开状态）
 */
export function useIframePointerEvents(active: boolean) {
  useEffect(() => {
    if (!active) return;

    const iframes = document.querySelectorAll<HTMLIFrameElement>('iframe');
    iframes.forEach(iframe => {
      iframe.style.pointerEvents = 'none';
    });

    return () => {
      const iframes = document.querySelectorAll<HTMLIFrameElement>('iframe');
      iframes.forEach(iframe => {
        iframe.style.pointerEvents = '';
      });
    };
  }, [active]);
}