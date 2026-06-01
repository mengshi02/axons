import { useState, useCallback } from 'react';
import {
  fetchNotifications,
  markNotificationRead,
  markAllNotificationsRead,
  deleteNotification as deleteNotificationApi,
  type Notification,
  type NotificationListResponse,
} from '../services/api';
import type { NotificationEvent } from './useEventStream';

export interface UseNotificationsReturn {
  notifications: Notification[];
  unreadCount: number;
  loading: boolean;
  total: number;
  fetchNotificationsList: (options?: { unread?: boolean; limit?: number; offset?: number }) => Promise<void>;
  markAsRead: (id: string) => Promise<void>;
  markAllAsRead: () => Promise<void>;
  deleteNotification: (id: string) => Promise<void>;
  handleNewNotification: (n: NotificationEvent) => void;
  refresh: () => Promise<void>;
}

export function useNotifications(): UseNotificationsReturn {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [unreadCount, setUnreadCount] = useState(0);
  const [loading, setLoading] = useState(true);
  const [total, setTotal] = useState(0);

  // Initial load
  const fetchNotificationsList = useCallback(async (options?: { unread?: boolean; limit?: number; offset?: number }) => {
    try {
      const data: NotificationListResponse = await fetchNotifications({
        limit: options?.limit ?? 50,
        offset: options?.offset ?? 0,
        unread: options?.unread,
      });
      setNotifications(data.notifications ?? []);
      setUnreadCount(data.unreadCount ?? 0);
      setTotal(data.total ?? 0);
      setLoading(false);
    } catch (err) {
      console.error('Failed to fetch notifications:', err);
      setLoading(false);
    }
  }, []);

  // Load on first call
  if (loading && notifications.length === 0) {
    fetchNotificationsList();
  }

  // SSE reconnect: full refresh
  const refresh = useCallback(async () => {
    try {
      const data = await fetchNotifications({ limit: 50 });
      setNotifications(data.notifications ?? []);
      setUnreadCount(data.unreadCount ?? 0);
      setTotal(data.total ?? 0);
    } catch (err) {
      console.error('Failed to refresh notifications:', err);
    }
  }, []);

  // SSE real-time update
  const handleNewNotification = useCallback((n: NotificationEvent) => {
    if (n.action === 'updated') {
      // Update existing entry (progress change or progress→terminal)
      setNotifications(prev => prev.map(item =>
        item.id === n.id
          ? { ...item, message: n.message, type: n.type, timestamp: n.timestamp }
          : item
      ));
      // No toast, no unreadCount increment
    } else {
      // New notification — prepend
      const newNotification: Notification = {
        id: n.id,
        source: n.source,
        type: n.type,
        title: n.title,
        message: n.message,
        group: '',
        actions: n.actions || [],
        read: n.read,
        timestamp: n.timestamp,
      };
      setNotifications(prev => [newNotification, ...prev]);
      setUnreadCount(prev => prev + 1);
    }
  }, []);

  const markAsRead = useCallback(async (id: string) => {
    try {
      await markNotificationRead(id);
      setNotifications(prev => prev.map(n => n.id === id ? { ...n, read: true } : n));
      setUnreadCount(prev => Math.max(0, prev - 1));
    } catch (err) {
      console.error('Failed to mark notification as read:', err);
    }
  }, []);

  const markAllAsRead = useCallback(async () => {
    try {
      await markAllNotificationsRead();
      setNotifications(prev => prev.map(n => ({ ...n, read: true })));
      setUnreadCount(0);
    } catch (err) {
      console.error('Failed to mark all notifications as read:', err);
    }
  }, []);

  const deleteNotification = useCallback(async (id: string) => {
    try {
      await deleteNotificationApi(id);
      setNotifications(prev => {
        const deleted = prev.find(n => n.id === id);
        if (deleted && !deleted.read) {
          setUnreadCount(c => Math.max(0, c - 1));
        }
        return prev.filter(n => n.id !== id);
      });
    } catch (err) {
      console.error('Failed to delete notification:', err);
    }
  }, []);

  return {
    notifications, unreadCount, loading, total,
    fetchNotificationsList, markAsRead, markAllAsRead, deleteNotification,
    handleNewNotification, refresh,
  };
}