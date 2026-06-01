// Sample vitest config file for testing
// This mimics the structure that caused parsing issues

import { defineConfig } from 'vitest/config';

export default defineConfig({
    test: {
        globals: true,
        environment: 'node',
        include: ['**/*_test.go'],
        coverage: {
            provider: 'v8',
            reporter: ['text', 'json', 'html'],
        },
    },
});

// Complex nested structure
const config = {
    nested: {
        deeply: {
            value: () => {
                return {
                    key: 'value'
                };
            }
        }
    }
};