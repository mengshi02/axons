const fs = require('fs');
const content = fs.readFileSync('CodeReferencesPanel.tsx', 'utf8');

const oldCode = `                  {/* Markdown preview mode */}
                  {filePath.toLowerCase().endsWith('.md') && mdPreviewMode ? (
                    <div className="p-6 prose prose-invert prose-sm max-w-none">
                      <ReactMarkdown remarkPlugins={[remarkGfm]}>
                        {codeContent}
                      </ReactMarkdown>
                    </div>
                  ) : (`;

const newCode = `                  {/* Markdown preview mode */}
                  {filePath.toLowerCase().endsWith('.md') && mdPreviewMode ? (
                    <div className="p-6 prose prose-invert prose-sm max-w-none">
                      <ReactMarkdown
                        remarkPlugins={[remarkGfm]}
                        components={{
                          text: ({ children }) => {
                            if (!searchQuery || typeof children !== 'string') return <>{children}</>;
                            const escaped = searchQuery.replace(/[.*+?^${}()|[\\]\\\\]/g, '\\\\$&');
                            const regex = new RegExp(\`(\${escaped})\`, 'gi');
                            const parts = children.split(regex);
                            return <>
                              {parts.map((part, i) => {
                                const isMatch = part.toLowerCase() === searchQuery.toLowerCase();
                                return isMatch
                                  ? <mark key={i} className="bg-yellow-300/50 dark:bg-yellow-400/30 px-0.5 rounded">{part}</mark>
                                  : part;
                              })}
                            </>;
                          }
                        }}
                      >
                        {codeContent}
                      </ReactMarkdown>
                    </div>
                  ) : (`;

const newContent = content.replace(oldCode, newCode);
fs.writeFileSync('CodeReferencesPanel.tsx', newContent);
console.log('Patched successfully');
