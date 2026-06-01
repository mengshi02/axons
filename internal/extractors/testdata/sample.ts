// Sample TypeScript file for testing
// This file is used by extractor tests

interface User {
    id: number;
    name: string;
    email?: string;
}

type UserRole = 'admin' | 'user' | 'guest';

export class UserService<T extends User> {
    private users: Map<number, T> = new Map();

    constructor(private name: string) {}

    addUser(user: T): void {
        this.users.set(user.id, user);
    }

    getUser(id: number): T | undefined {
        return this.users.get(id);
    }

    async fetchUser(id: number): Promise<T> {
        return this.users.get(id)!;
    }
}

export function createService(name: string): UserService<User> {
    return new UserService<User>(name);
}

export const DEFAULT_ROLE: UserRole = 'user';

// Arrow function with type annotation
const typedArrow = (x: number, y: number): number => x + y;

// Generic function
function identity<T>(arg: T): T {
    return arg;
}

export { UserService, createService };